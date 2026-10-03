import SwiftUI
import IchorCore

/// A node's storage (os:reader): mounted filesystems, Talos volumes, a disk usage explorer and
/// the disks' health. Each part loads on its own, so one that this Talos version lacks only
/// shows its notice.
struct StorageView: View {
    let node: String
    let hostname: String

    // Explicit: the private @State properties make the memberwise init private.
    init(node: String, hostname: String) {
        self.node = node
        self.hostname = hostname
    }

    @Environment(AppModel.self) private var model
    @State private var mounts: LoadState<NodeMounts> = .loading
    @State private var volumes: LoadState<NodeVolumes> = .loading
    @State private var health: LoadState<NodeDiskHealth> = .loading
    /// The directory the disk usage explorer shows; nil until the user picks one (measuring a
    /// large tree takes minutes, so nothing is measured unasked).
    @State private var usagePath: String?
    @State private var usage: LoadState<DiskUsage> = .loading
    @State private var showAllMounts = false

    var body: some View {
        List {
            Section("Mounts") {
                SectionStateView(state: mounts) { loaded in
                    let shown = listedMounts(loaded.mounts, showAll: showAllMounts)
                    if shown.isEmpty {
                        Text("No mounted filesystem reported").font(.footnote).foregroundStyle(.secondary)
                    }
                    ForEach(shown) { MountRow(mount: $0) }
                    // A node reports hundreds of per-pod and pseudo mounts: on request only.
                    if loaded.mounts.count > listedMounts(loaded.mounts, showAll: false).count {
                        Toggle("Show all mounts (\(loaded.mounts.count))", isOn: $showAllMounts).font(.footnote)
                    }
                }
            }
            Section("Volumes") {
                if let notice = model.support(.volumes, node: node).localizedNotice {
                    VersionNoticeRow(text: notice)
                } else {
                    SectionStateView(state: volumes) { loaded in
                        if !loaded.supported {
                            VersionNoticeRow(text: volumesNotice(loaded))
                        } else if loaded.volumes.isEmpty {
                            Text("No volumes").font(.footnote).foregroundStyle(.secondary)
                        }
                        ForEach(loaded.volumes) { VolumeRow(volume: $0) }
                    }
                }
            }
            DiskUsageSection(path: usagePath, state: usage, support: model.support(.diskUsage, node: node)) { target in
                // A new path restarts the usage task below; nil (Cancel) stops waiting for it.
                usage = .loading
                usagePath = target
            }
            DiskHealthSection(state: health, support: model.support(.diskHealth, node: node))
        }
        .refreshable {
            // Also what the node's Talos version can do: Volumes and Disk health are asked
            // again even when they were not available before (the node may have been upgraded).
            await model.reloadFeatures(node: node)
            await loadMounts()
            await loadVolumes()
            await loadHealth()
            // The folder being shown, if it was measured: one still being measured can take minutes.
            if case .loaded = usage { await loadUsage() }
        }
        .themedBackground()
        .navigationTitle(String(localized: "Storage · \(hostname)"))
        .navigationBarTitleDisplayMode(.inline)
        // Independent reads, so a slow one does not hold back the others. On the list rather
        // than on rows, which come and go while scrolling.
        .task { await loadMounts() }
        .task { await loadVolumes() }
        .task { await loadHealth() }
        .task(id: usagePath) { await loadUsage() }
    }

    private func volumesNotice(_ volumes: NodeVolumes) -> String {
        FeatureSupport(supported: false, reason: volumes.reason).localizedNotice
            ?? String(localized: "Not available on this node's Talos version")
    }

    private func loadMounts() async {
        guard let client = model.client else { return }
        let result: LoadState<NodeMounts> = await .from { try await client.mounts(node: node) }
        if !Task.isCancelled { mounts = mounts.refreshed(with: result) }
    }

    /// What the node's Talos version lacks is not asked for (the section shows the notice).
    private func loadVolumes() async {
        guard let client = model.client else { return }
        await model.loadFeatures(node: node)
        guard model.support(.volumes, node: node).supported else { return }
        let result: LoadState<NodeVolumes> = await .from { try await client.volumes(node: node) }
        if !Task.isCancelled { volumes = volumes.refreshed(with: result) }
    }

    private func loadUsage() async {
        guard let client = model.client else { return }
        await model.loadFeatures(node: node)
        guard let wanted = usagePath, model.support(.diskUsage, node: node).supported else { return }
        let result: LoadState<DiskUsage> = await .from { try await client.diskUsage(node: node, path: wanted) }
        if wanted == usagePath, !Task.isCancelled { usage = result } // a newer path wins
    }

    private func loadHealth() async {
        guard let client = model.client else { return }
        await model.loadFeatures(node: node)
        guard model.support(.diskHealth, node: node).supported else { return }
        let result: LoadState<NodeDiskHealth> = await .from { try await client.diskHealth(node: node) }
        if !Task.isCancelled { health = health.refreshed(with: result) }
    }
}

/// Spinner / error or version notice / content, as rows of a list section.
struct SectionStateView<T, Content: View>: View {
    let state: LoadState<T>
    @ViewBuilder let content: (T) -> Content

    var body: some View {
        switch state {
        case .loading:
            HStack(spacing: 8) {
                ProgressView()
                Text("Loading…").foregroundStyle(.secondary)
            }
        case .failed(let message):
            ErrorOrNoticeText(message: message)
        case .loaded(let value, _, _):
            content(value)
        }
    }
}

private struct MountRow: View {
    let mount: MountInfo

    var body: some View {
        let level = usageLevel(percent: mount.usedPercent)
        VStack(alignment: .leading, spacing: 4) {
            HStack(alignment: .firstTextBaseline) {
                Text(verbatim: mount.mountedOn).font(.callout.monospaced()).lineLimit(1).truncationMode(.middle)
                Spacer()
                Text(verbatim: "\(Int(mount.usedPercent.rounded())) %")
                    .font(.subheadline.weight(.semibold))
                    .monospacedDigit()
                    .foregroundStyle(level == .normal ? Color.primary : level.color)
            }
            UsageLevelBar(percent: mount.usedPercent)
            HStack(alignment: .firstTextBaseline) {
                Text(verbatim: mount.filesystem).font(.caption.monospaced()).lineLimit(1).truncationMode(.middle)
                Spacer()
                Text(verbatim: "\(formatBytes(mount.used)) / \(formatBytes(mount.size))").monospacedDigit()
            }
            .font(.caption)
            .foregroundStyle(.secondary)
            Text("\(formatBytes(mount.available)) free").font(.caption).foregroundStyle(.secondary)
        }
        .accessibilityElement(children: .combine)
    }
}

private struct VolumeRow: View {
    let volume: VolumeInfo

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Text(verbatim: volume.id).font(.headline)
                Spacer()
                if !volume.phase.isEmpty {
                    StatusPill(label: volume.phase, color: volume.isReady ? .green : .orange)
                }
            }
            let facts = [volume.type, volume.filesystem, volume.size > 0 ? formatBytes(volume.size) : ""].filter { !$0.isEmpty }
            if !facts.isEmpty {
                Text(verbatim: facts.joined(separator: "  ·  ")).font(.caption)
            }
            if !volume.location.isEmpty {
                Text(verbatim: volume.location).font(.caption.monospaced()).foregroundStyle(.secondary)
                    .lineLimit(1).truncationMode(.middle)
            }
            if !volume.mountedOn.isEmpty {
                LabeledContent("Mounted on") { Text(verbatim: volume.mountedOn).font(.caption.monospaced()) }.font(.caption)
            }
            if !volume.error.isEmpty {
                Text(verbatim: volume.error).font(.caption).foregroundStyle(.red)
            }
            if !volume.encryption.isEmpty {
                Label {
                    Text("Encrypted (\(volume.encryption))")
                } icon: {
                    Image(systemName: "lock.fill")
                }
                .font(.caption)
                .foregroundStyle(.secondary)
            }
        }
    }
}
