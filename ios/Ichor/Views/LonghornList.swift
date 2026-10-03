import SwiftUI
import IchorCore

/// Longhorn volumes (problems first), the backup targets and each node's disks.
struct LonghornList: View {
    let status: LonghornStatus
    let refresh: () async -> Void

    @State private var filter: VolumeFilter?
    @State private var query = ""

    var body: some View {
        let hasProblems = status.volumes.contains { $0.health.needsAttention }
        let current = filter ?? (hasProblems ? .problems : .all)
        let rows = filterVolumes(status.volumes, filter: current, query: query)
        List {
            if !status.error.isEmpty { Section { ErrorLine(error: status.error) } }
            if !status.backupTargets.isEmpty {
                Section { ForEach(status.backupTargets) { BackupTargetRow(target: $0) } }
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
                ForEach(rows) { VolumeRow(volume: $0) }
            }
            if !status.nodes.isEmpty {
                Section("Nodes") { ForEach(status.nodes) { NodeRow(node: $0) } }
            }
        }
        .searchable(text: $query, prompt: Text("Filter by namespace or name"))
        .refreshable { await refresh() }
        .themedBackground()
    }
}

private struct VolumeRow: View {
    let volume: LonghornVolume

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            HealthDot(health: volume.health).padding(.top, 5)
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: volume.label).font(.subheadline.monospaced()).lineLimit(1)
                Text(verbatim: facts).font(.caption).foregroundStyle(volume.health.needsAttention ? volume.health.color : .secondary)
                Text(verbatim: details).font(.caption).foregroundStyle(.secondary).lineLimit(2).monospacedDigit()
            }
        }
    }

    private var facts: String {
        var parts = [stateLabel(volume.state)]
        if volume.health == .critical || (!volume.robustness.isEmpty && volume.robustness != "unknown") {
            parts.append(robustnessLabel(volume.robustness))
        }
        parts.append(String(localized: "\(volume.replicasHealthy)/\(volume.replicasDesired) replicas"))
        if volume.rebuilding > 0 { parts.append(String(localized: "\(volume.rebuilding) rebuilding")) }
        return parts.joined(separator: " · ")
    }

    private var details: String {
        var parts: [String] = []
        if !volume.node.isEmpty {
            parts.append(String(localized: "on \(volume.node)"))
        } else if !volume.replicaNodes.isEmpty {
            parts.append(volume.replicaNodes.joined(separator: ", "))
        }
        parts.append("\(formatBytes(volume.actualSize)) / \(formatBytes(volume.size))")
        parts.append(volume.lastBackupAt > 0 ? String(localized: "backup \(relativeTime(volume.lastBackupAt))") : String(localized: "never backed up"))
        return parts.joined(separator: " · ")
    }
}

private struct BackupTargetRow: View {
    let target: LonghornBackupTarget

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            HStack {
                VStack(alignment: .leading, spacing: 1) {
                    Text("Backup target").font(.subheadline)
                    Text(verbatim: target.url).font(.caption.monospaced()).foregroundStyle(.secondary)
                }
                Spacer()
                StatusPill(label: target.available ? String(localized: "available") : String(localized: "unavailable"),
                           color: target.available ? .green : .red)
            }
            if !target.available && !target.message.isEmpty {
                Text(verbatim: target.message).font(.caption).foregroundStyle(.red)
            }
        }
    }
}

private struct NodeRow: View {
    let node: LonghornNode

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                HealthDot(health: node.ready ? .ok : .critical)
                Text(verbatim: node.name).font(.subheadline.monospaced())
                Spacer()
                if !node.ready {
                    Text("not ready").font(.caption).foregroundStyle(.red)
                } else if !node.schedulable {
                    Text("scheduling disabled").font(.caption).foregroundStyle(attentionColor)
                }
            }
            ForEach(Array(node.disks.enumerated()), id: \.offset) { _, disk in
                if disk.maximum > 0 {
                    ProgressView(value: min(1, Double(disk.scheduled) / Double(disk.maximum))).tint(disk.schedulable ? .accentColor : attentionColor)
                }
                Text(verbatim: [disk.path.isEmpty ? nil : disk.path,
                                String(localized: "\(formatBytes(disk.scheduled)) of \(formatBytes(disk.maximum)) scheduled"),
                                disk.schedulable ? nil : String(localized: "scheduling disabled")].compactMap { $0 }.joined(separator: " · "))
                    .font(.caption).foregroundStyle(.secondary)
            }
        }
    }
}

private func stateLabel(_ state: String) -> String {
    switch state {
    case "attached": String(localized: "attached")
    case "detached": String(localized: "detached")
    case "attaching": String(localized: "attaching")
    case "detaching": String(localized: "detaching")
    case "creating": String(localized: "creating")
    case "deleting": String(localized: "deleting")
    default: state
    }
}

private func robustnessLabel(_ robustness: String) -> String {
    switch robustness {
    case "healthy": String(localized: "healthy")
    case "degraded": String(localized: "degraded")
    case "faulted": String(localized: "faulted")
    default: String(localized: "unknown")
    }
}
