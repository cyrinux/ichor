import SwiftUI
import IchorCore

/// Longhorn volumes (problems first), the backup targets and each node's disks, with the volume
/// actions (backup, trim, replica count, snapshots, attach or detach, salvage, engine upgrade) and the node ones
/// (scheduling, eviction) in each row's context menu and swipe actions: none sits in a row.
struct LonghornList: View {
    let status: LonghornStatus
    let refresh: () async -> Void

    @Environment(AppModel.self) private var model
    @State private var filter: VolumeFilter?
    @State private var query = ""
    @State private var attachmentTarget: LonghornAttachmentTarget?
    @State private var salvageTarget: LonghornVolume?
    @State private var engineTarget: LonghornVolume?
    @State private var snapshotsFor: LonghornVolumeName?
    @State private var replicasFor: LonghornVolume?
    @State private var confirmEvict: LonghornNode?
    @State private var running: Set<String> = []
    @State private var resultMessage: String?

    var body: some View {
        let hasProblems = status.volumes.contains { $0.health.needsAttention }
        let current = filter ?? (hasProblems ? .problems : .all)
        let rows = filterVolumes(status.volumes, filter: current, query: query)
        List {
            if !status.error.isEmpty { Section { ErrorLine(error: status.error) } }
            if !status.backupTargets.isEmpty {
                Section { ForEach(status.backupTargets) { LonghornBackupTargetRow(target: $0) } }
            }
            if status.outdatedEngineVolumes > 0 {
                Section {
                    Label {
                        Text("Volumes on an old engine: \(String(status.outdatedEngineVolumes))")
                    } icon: {
                        Image(systemName: "arrow.up.circle")
                    }
                    .foregroundStyle(.secondary)
                }
            }
            Section {
                Picker(selection: Binding(get: { current }, set: { filter = $0 })) {
                    Text("Problems").tag(VolumeFilter.problems)
                    Text("All").tag(VolumeFilter.all)
                    Text("Detached").tag(VolumeFilter.detached)
                } label: {
                    EmptyView()
                }
                .pickerStyle(.segmented)
                if rows.isEmpty {
                    Group {
                        if !query.isEmpty {
                            Text("Nothing matches “\(query)”.")
                        } else if current == .problems {
                            Text("Nothing needs attention.")
                        } else {
                            Text("No volumes.")
                        }
                    }
                    .foregroundStyle(.secondary)
                }
                ForEach(rows) { volumeRow($0) }
            }
            if !status.nodes.isEmpty {
                let sharedPath = longhornSharedDiskPath(status.nodes)
                Section {
                    ForEach(status.nodes) { nodeRow($0, sharedPath: sharedPath) }
                } header: {
                    Text("Nodes")
                } footer: {
                    // Said once when every disk sits on it, not on each node.
                    if let sharedPath {
                        Label { Text(verbatim: sharedPath).monospaced() } icon: { Image(systemName: "internaldrive") }
                    }
                }
            }
        }
        .searchable(text: $query, prompt: Text("Filter by namespace or name"))
        .refreshable { await refresh() }
        .themedBackground()
        .sheet(item: $replicasFor) { volume in
            LonghornReplicaSheet(volume: volume, nodes: status.nodes.count) { count in
                Task { await run(LonghornRequest(volume: volume, action: .replicas, value: count)) }
            }
        }
        .confirmationDialog(confirmEvict.map { String(localized: "Evict the replicas of \($0.name)?") } ?? "",
                            isPresented: $confirmEvict.isPresent(),
                            titleVisibility: .visible,
                            presenting: confirmEvict) { node in
            Button("Evict replicas", role: .destructive) {
                Task { await run(LonghornRequest(node: node, action: .evict)) }
            }
            Button("Cancel", role: .cancel) {}
        } message: { node in
            Text("Longhorn copies every replica on \(node.name) to other nodes, then removes them. Scheduling stays off until you turn it back on.")
        }
        .sheet(item: $snapshotsFor) { name in
            // Looked up by name: the sheet follows the volume as the status refreshes.
            if let volume = status.volumes.first(where: { $0.name == name.id }) {
                LonghornSnapshotsSheet(volume: volume, refresh: refresh).loadsKubeActionAccess(namespace: volume.namespace)
            } else {
                // The volume is gone: no blank sheet.
                Color.clear.onAppear { snapshotsFor = nil }
            }
        }
        .sheet(item: $salvageTarget) { volume in
            LonghornSalvageSheet(volume: volume) { names in
                salvageTarget = nil
                Task { await salvage(volume, names: names) }
            }.loadsKubeActionAccess(namespace: volume.namespace)
        }
        .sheet(item: $engineTarget) { volume in
            LonghornEngineSheet(volume: volume) { image in
                engineTarget = nil
                Task { await engineUpgrade(volume, image: image) }
            }.loadsKubeActionAccess(namespace: volume.namespace)
        }
        .sheet(item: $attachmentTarget) { target in
            LonghornAttachmentSheet(target: target) { node in
                attachmentTarget = nil
                Task { await attachment(target, node: node) }
            }.loadsKubeActionAccess(namespace: target.volume.namespace)
        }
        .messageAlert($resultMessage)
    }

    private func volumeRow(_ volume: LonghornVolume) -> some View {
        let busy = running.contains(LonghornRequest.key(namespace: volume.namespace, name: volume.name, node: false))
        let actions = busy ? [] : volume.actions
        let act = { (action: LonghornAction) in
            if action == .replicas { replicasFor = volume } else { Task { await run(LonghornRequest(volume: volume, action: action)) } }
        }
        let sheets = { (inMenu: Bool) in
            LonghornVolumeSheetButtons(volume: volume, busy: busy, inMenu: inMenu,
                                       onSnapshots: { snapshotsFor = LonghornVolumeName(id: volume.name) },
                                       onSalvage: { salvageTarget = volume },
                                       onAttachment: { attach in attachmentTarget = LonghornAttachmentTarget(volume: volume, attach: attach) },
                                       onEngineUpgrade: { engineTarget = volume })
        }
        return LonghornVolumeRow(volume: volume, busy: busy, availableEngine: status.defaultEngineVersion) { snapshotsFor = LonghornVolumeName(id: volume.name) }
            .swipeActions(edge: .leading) { sheets(false) }
            .swipeActions(edge: .trailing) { LonghornActionButtons(actions: actions, namespace: volume.namespace, onAction: act) }
            .contextMenu {
                sheets(true)
                LonghornActionButtons(actions: actions, namespace: volume.namespace, inMenu: true, onAction: act)
            }
            // Last: the buttons above read the access it loads.
            .loadsKubeActionAccess(namespace: volume.namespace)
    }

    private func nodeRow(_ node: LonghornNode, sharedPath: String?) -> some View {
        let busy = running.contains(LonghornRequest.key(namespace: node.namespace, name: node.name, node: true))
        let actions = busy ? [] : node.actions
        let act = { (action: LonghornAction) in
            if action == .evict { confirmEvict = node } else { Task { await run(LonghornRequest(node: node, action: action)) } }
        }
        return LonghornNodeRow(node: node, busy: busy, sharedPath: sharedPath)
            .swipeActions(edge: .trailing) { LonghornActionButtons(actions: actions, namespace: node.namespace, onAction: act) }
            .contextMenu { LonghornActionButtons(actions: actions, namespace: node.namespace, inMenu: true, onAction: act) }
            .loadsKubeActionAccess(namespace: node.namespace)
    }

    private func salvage(_ volume: LonghornVolume, names: Set<String>) async {
        let key = LonghornRequest.key(namespace: volume.namespace, name: volume.name, node: false)
        guard let client = model.client, !running.contains(key) else { return }
        running.insert(key)
        defer { running.remove(key) }
        do {
            try await client.longhornSalvage(namespace: volume.namespace, name: volume.name, replicas: names)
            resultMessage = String(localized: "Salvage requested")
            await refresh()
        } catch { resultMessage = String(localized: "Could not salvage volume: \(error.localizedDescription)") }
    }

    private func attachment(_ target: LonghornAttachmentTarget, node: String) async {
        let volume = target.volume
        let key = LonghornRequest.key(namespace: volume.namespace, name: volume.name, node: false)
        guard let client = model.client, !running.contains(key) else { return }
        running.insert(key)
        defer { running.remove(key) }
        do {
            try await client.longhornAttachment(namespace: volume.namespace, name: volume.name, node: node, attach: target.attach)
            resultMessage = String(localized: "Volume attachment change requested")
            await refresh()
        } catch { resultMessage = String(localized: "Could not change volume attachment: \(error.localizedDescription)") }
    }

    private func engineUpgrade(_ volume: LonghornVolume, image: String) async {
        let key = LonghornRequest.key(namespace: volume.namespace, name: volume.name, node: false)
        guard let client = model.client, !running.contains(key) else { return }
        running.insert(key)
        defer { running.remove(key) }
        do {
            try await client.longhornEngineUpgrade(namespace: volume.namespace, name: volume.name, image: image)
            resultMessage = String(localized: "Engine upgrade requested")
            await refresh()
        } catch { resultMessage = String(localized: "Could not upgrade the engine: \(error.localizedDescription)") }
    }

    private func run(_ request: LonghornRequest) async {
        guard let client = model.client, !running.contains(request.id) else { return }
        running.insert(request.id)
        defer { running.remove(request.id) }
        do {
            try await client.longhornAction(namespace: request.namespace, name: request.name, action: request.action, value: request.value)
            resultMessage = request.doneMessage
            await refresh()
        } catch {
            resultMessage = String(localized: "Could not change \(request.label): \(error.localizedDescription)")
        }
    }
}

/// The volume the snapshots sheet shows, by name.
private struct LonghornVolumeName: Identifiable {
    let id: String
}

/// What a volume opens a sheet for, in its context menu and leading swipe actions: its snapshots
/// and backups, then salvage when it is faulted, else attach or detach, and in the menu an engine
/// upgrade when the volume lags the default engine. The sheets confirm. None
/// while an action on the volume runs, but the snapshots.
private struct LonghornVolumeSheetButtons: View {
    let volume: LonghornVolume
    let busy: Bool
    let inMenu: Bool
    let onSnapshots: () -> Void
    let onSalvage: () -> Void
    let onAttachment: (_ attach: Bool) -> Void
    let onEngineUpgrade: () -> Void

    private var faulted: Bool { volume.robustness == "faulted" }
    private var detached: Bool { volume.state == "detached" }
    /// Attach a detached volume that is not faulted, detach an attached one.
    private var attachmentChange: Bool { volume.state == "attached" || (detached && !faulted) }

    var body: some View {
        Button(action: onSnapshots) { Label("Snapshots & backups", systemImage: "camera") }
            .tint(.indigo)
        if !busy && faulted {
            if inMenu {
                KubeGatedMenuButton(String(localized: "Salvage volume"), systemImage: "cross.case",
                                    action: .longhornSalvage, in: volume.namespace, perform: onSalvage)
            } else {
                Button(action: onSalvage) { Label("Salvage volume", systemImage: "cross.case") }
                    .tint(.orange)
                    .kubeGated(.longhornSalvage, in: volume.namespace)
            }
        }
        if !busy && attachmentChange {
            let title = detached ? String(localized: "Attach volume") : String(localized: "Detach volume")
            let icon = detached ? "link" : "eject"
            if inMenu {
                KubeGatedMenuButton(title, systemImage: icon, action: .longhornAttachment, in: volume.namespace) { onAttachment(detached) }
            } else {
                Button { onAttachment(detached) } label: { Label(title, systemImage: icon) }
                    .tint(.blue)
                    .kubeGated(.longhornAttachment, in: volume.namespace)
            }
        }
        // Menu only: the leading swipe already holds two buttons.
        if !busy && inMenu && volume.engineUpgradable {
            KubeGatedMenuButton(String(localized: "Upgrade engine"), systemImage: "arrow.up.circle",
                                action: .longhornEngineUpgrade, in: volume.namespace, perform: onEngineUpgrade)
        }
    }
}
