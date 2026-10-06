import SwiftUI
import IchorCore

/// Each Garage cluster: its state, why, the sync counters and its nodes (down ones first), with
/// the blocks failing to resync and each node's resync speed when the CLI could be read.
struct GarageList: View {
    let status: GarageStatus
    let refresh: () async -> Void

    @Environment(AppModel.self) private var model
    @State private var confirm: TranquilityRequest?
    @State private var setting: Set<String> = []
    @State private var resultMessage: String?

    var body: some View {
        List {
            if !status.error.isEmpty { Section { ErrorLine(error: status.error) } }
            ForEach(status.instances) { instance in
                InstanceSection(instance: instance, setting: setting) { confirm = $0 }
            }
        }
        .refreshable { await refresh() }
        .themedBackground()
        .confirmationDialog(confirm.map(\.title) ?? "",
                            isPresented: Binding(get: { confirm != nil }, set: { if !$0 { confirm = nil } }),
                            titleVisibility: .visible,
                            presenting: confirm) { request in
            Button {
                Task { await setTranquility(request) }
            } label: {
                if request.fullSpeed { Text("Resync at full speed") } else { Text("Default resync speed") }
            }
            Button("Cancel", role: .cancel) {}
        } message: { request in
            if request.fullSpeed {
                Text("The node resyncs missing blocks without pausing between them: faster recovery, but more disk and network IO while it catches up.")
            } else {
                Text("The node goes back to Garage's default resync pace, lighter on disk and network IO.")
            }
        }
        .alert(resultMessage ?? "", isPresented: Binding(get: { resultMessage != nil }, set: { if !$0 { resultMessage = nil } })) {
            Button("OK") {}
        }
    }

    private func setTranquility(_ request: TranquilityRequest) async {
        guard let client = model.client, !setting.contains(request.id) else { return }
        setting.insert(request.id)
        defer { setting.remove(request.id) }
        do {
            try await client.garageSetTranquility(request.instance, node: request.node.nodeID, value: request.value)
            resultMessage = request.fullSpeed
                ? String(localized: "\(request.node.label) now resyncs at full speed")
                : String(localized: "\(request.node.label) is back to the default resync speed")
            await refresh()
        } catch {
            resultMessage = String(localized: "Could not change the resync speed of \(request.node.label): \(error.localizedDescription)")
        }
    }
}

/// A resync tranquility change asked for one Garage node: 0 is full speed, 2 Garage's default.
private struct TranquilityRequest: Identifiable {
    let instance: GarageInstance
    let node: GarageNode
    let value: Int

    var id: String { "\(instance.id)|\(node.id)" }
    var fullSpeed: Bool { Int64(value) == GarageTranquility.fullSpeed }

    var title: String {
        fullSpeed
            ? String(localized: "Resync \(node.label) at full speed?")
            : String(localized: "Default resync speed on \(node.label)?")
    }

    /// The change a node offers: full speed unless it already resyncs at full speed. Only for an
    /// up storage node of a cluster read through its CLI, with a pod to run the command in.
    init?(instance: GarageInstance, node: GarageNode) {
        guard instance.detailed, !instance.pod.isEmpty, node.up, node.storage, !node.nodeID.isEmpty else { return nil }
        self.instance = instance
        self.node = node
        value = Int(node.tranquility == GarageTranquility.fullSpeed ? GarageTranquility.standard : GarageTranquility.fullSpeed)
    }
}

private struct InstanceSection: View {
    let instance: GarageInstance
    let setting: Set<String>
    let onTranquility: (TranquilityRequest) -> Void

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
                    Text("Only the basic status is available: \(instance.message.or("—"))")
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
            if instance.detailed && !instance.pod.isEmpty {
                NavigationLink {
                    GarageBlockErrorsView(instance: instance)
                } label: {
                    Label("Block errors", systemImage: "cube.transparent")
                }
            }
            ForEach(instance.nodes) { node in
                let request = TranquilityRequest(instance: instance, node: node)
                let busy = request.map { setting.contains($0.id) } ?? false
                NodeRow(node: node, busy: busy)
                    .swipeActions(edge: .trailing) {
                        if let request, !busy {
                            Button { onTranquility(request) } label: { TranquilityLabel(fullSpeed: request.fullSpeed) }
                                .tint(request.fullSpeed ? .orange : .blue)
                        }
                    }
                    .contextMenu {
                        if let request, !busy {
                            Button { onTranquility(request) } label: { TranquilityLabel(fullSpeed: request.fullSpeed) }
                        }
                    }
            }
        } header: {
            HStack {
                Text(verbatim: instance.label).textCase(nil)
                Spacer()
                StatusPill(label: instance.state.label, color: instance.state.health.color)
            }
        }
    }
}

private struct TranquilityLabel: View {
    let fullSpeed: Bool

    var body: some View {
        if fullSpeed {
            Label("Resync at full speed", systemImage: "hare")
        } else {
            Label("Default resync speed", systemImage: "tortoise")
        }
    }
}

private struct NodeRow: View {
    let node: GarageNode
    let busy: Bool

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
                if busy { ProgressView() }
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
        .accessibilityElement(children: .combine)
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
        if node.tranquility == GarageTranquility.fullSpeed {
            parts.append(String(localized: "resync: full speed"))
        } else if node.tranquility > 0 {
            parts.append(String(localized: "resync tranquility \(node.tranquility)"))
        }
        return parts.joined(separator: " · ")
    }
}
