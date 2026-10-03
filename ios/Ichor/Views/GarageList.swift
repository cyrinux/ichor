import SwiftUI
import IchorCore

/// Each Garage cluster: its state, why, the sync counters and its nodes (down ones first).
struct GarageList: View {
    let status: GarageStatus
    let refresh: () async -> Void

    var body: some View {
        List {
            if !status.error.isEmpty { Section { ErrorLine(error: status.error) } }
            ForEach(status.instances) { InstanceSection(instance: $0) }
        }
        .refreshable { await refresh() }
        .themedBackground()
    }
}

private struct InstanceSection: View {
    let instance: GarageInstance

    var body: some View {
        Section {
            if instance.detailed {
                if !instance.message.isEmpty {
                    Text(verbatim: instance.message)
                        .font(.caption)
                        .foregroundStyle(instance.state.health.needsAttention ? instance.state.health.color : .secondary)
                }
            } else {
                Label {
                    Text("Only the basic status is available: \(instance.message.isEmpty ? "—" : instance.message)")
                } icon: {
                    Image(systemName: "info.circle")
                }
                .font(.caption)
                .foregroundStyle(.secondary)
            }
            if instance.storageNodes > 0 {
                LabeledContent("Storage nodes", value: String(localized: "\(instance.storageNodesUp)/\(instance.storageNodes) up"))
            }
            if instance.partitions > 0 {
                LabeledContent("Fully replicated", value: "\(instance.partitionsAllOk)/\(instance.partitions)")
                LabeledContent("Write quorum", value: "\(instance.partitionsQuorum)/\(instance.partitions)")
            }
            if instance.resyncQueue >= 0 { LabeledContent("Resync queue", value: instance.resyncQueue.formatted()) }
            if instance.resyncErrors >= 0 {
                LabeledContent("Blocks failing to resync") {
                    Text(verbatim: instance.resyncErrors.formatted()).foregroundStyle(instance.resyncErrors > 0 ? .red : .secondary)
                }
            }
            if instance.tableSyncQueue >= 0 { LabeledContent("Metadata sync queue", value: instance.tableSyncQueue.formatted()) }
            LabeledContent("Pods", value: String(localized: "\(instance.podsReady)/\(instance.pods) ready"))
            if !instance.version.isEmpty { LabeledContent("Version", value: instance.version) }
            ForEach(instance.nodes) { NodeRow(node: $0) }
        } header: {
            HStack {
                Text(verbatim: instance.label).textCase(nil)
                Spacer()
                StatusPill(label: instance.state.label, color: instance.state.health.color)
            }
        }
    }
}

private struct NodeRow: View {
    let node: GarageNode

    private var down: Bool { !node.up && node.storage }

    private var health: ServiceHealth {
        if down { return .critical }
        if !node.up { return .idle }
        if node.resyncErrors > 0 || !node.statsError.isEmpty { return .warning }
        return .ok
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack {
                HealthDot(health: health)
                Text(verbatim: node.label).font(.subheadline.monospaced()).lineLimit(1)
                Spacer()
                if !node.zone.isEmpty { Text(verbatim: node.zone).font(.caption).foregroundStyle(.secondary) }
            }
            if !details.isEmpty {
                Text(verbatim: details).font(.caption).foregroundStyle(down ? .red : .secondary).monospacedDigit()
            }
            if node.dataTotal > 0 {
                ProgressView(value: Double(node.dataTotal - node.dataAvail), total: Double(node.dataTotal))
                Text(verbatim: "\(formatBytes(node.dataTotal - node.dataAvail)) / \(formatBytes(node.dataTotal))")
                    .font(.caption).foregroundStyle(.secondary)
            }
        }
    }

    private var details: String {
        var parts: [String] = []
        if !node.up {
            parts.append(node.lastSeenSecs >= 0
                ? String(localized: "down, last seen \(formatDuration(node.lastSeenSecs)) ago")
                : String(localized: "down"))
        }
        if !node.kubeNode.isEmpty { parts.append(String(localized: "on \(node.kubeNode)")) }
        if node.resyncQueue >= 0 { parts.append(String(localized: "Resync queue") + " " + node.resyncQueue.formatted()) }
        if node.resyncErrors > 0 { parts.append(String(localized: "Blocks failing to resync") + " " + node.resyncErrors.formatted()) }
        if !node.statsError.isEmpty && node.up { parts.append(String(localized: "no statistics: \(node.statsError)")) }
        return parts.joined(separator: " · ")
    }
}
