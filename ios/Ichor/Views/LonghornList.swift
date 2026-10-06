import SwiftUI
import IchorCore

/// Longhorn volumes (problems first), the backup targets and each node's disks, with the volume
/// actions (backup, trim, replica count) and the node ones (scheduling, eviction) in each row's
/// context menu and swipe actions.
struct LonghornList: View {
    let status: LonghornStatus
    let refresh: () async -> Void

    @Environment(AppModel.self) private var model
    @State private var filter: VolumeFilter?
    @State private var query = ""
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
                Section("Nodes") { ForEach(status.nodes) { nodeRow($0) } }
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
        .messageAlert($resultMessage)
    }

    private func volumeRow(_ volume: LonghornVolume) -> some View {
        let busy = running.contains(LonghornRequest.key(namespace: volume.namespace, name: volume.name, node: false))
        let actions = busy ? [] : volume.actions
        let act = { (action: LonghornAction) in
            if action == .replicas { replicasFor = volume } else { Task { await run(LonghornRequest(volume: volume, action: action)) } }
        }
        return LonghornVolumeRow(volume: volume, busy: busy)
            .swipeActions(edge: .trailing) { LonghornActionButtons(actions: actions, onAction: act) }
            .contextMenu { LonghornActionButtons(actions: actions, inMenu: true, onAction: act) }
    }

    private func nodeRow(_ node: LonghornNode) -> some View {
        let busy = running.contains(LonghornRequest.key(namespace: node.namespace, name: node.name, node: true))
        let actions = busy ? [] : node.actions
        let act = { (action: LonghornAction) in
            if action == .evict { confirmEvict = node } else { Task { await run(LonghornRequest(node: node, action: action)) } }
        }
        return LonghornNodeRow(node: node, busy: busy)
            .swipeActions(edge: .trailing) { LonghornActionButtons(actions: actions, onAction: act) }
            .contextMenu { LonghornActionButtons(actions: actions, inMenu: true, onAction: act) }
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
