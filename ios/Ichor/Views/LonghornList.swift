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
    @State private var attachmentTarget: LonghornAttachmentTarget?
    @State private var salvageTarget: LonghornVolume?
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
        return VStack(alignment: .leading) {
            LonghornVolumeRow(volume: volume, busy: busy)
            LonghornProtectionLine(volume: volume) { snapshotsFor = LonghornVolumeName(id: volume.name) }
                .padding(.leading, 26)
            if volume.state == "attached" || (volume.state == "detached" && volume.robustness != "faulted") {
                Button { attachmentTarget = LonghornAttachmentTarget(volume: volume, attach: volume.state == "detached") } label: {
                    if volume.state == "detached" { Text("Attach volume") } else { Text("Detach volume") }
                }.disabled(busy).kubeGated(.longhornAttachment, in: volume.namespace)
                KubeDeniedNote(.longhornAttachment, in: volume.namespace)
            }
            if volume.robustness == "faulted" {
                Button { salvageTarget = volume } label: { Text("Salvage volume") }
                    .disabled(busy).kubeGated(.longhornSalvage, in: volume.namespace)
                KubeDeniedNote(.longhornSalvage, in: volume.namespace)
            }
        }
            .swipeActions(edge: .trailing) { LonghornActionButtons(actions: actions, namespace: volume.namespace, onAction: act) }
            .contextMenu { LonghornActionButtons(actions: actions, namespace: volume.namespace, inMenu: true, onAction: act) }
            // Last: the buttons above read the access it loads.
            .loadsKubeActionAccess(namespace: volume.namespace)
    }

    private func nodeRow(_ node: LonghornNode) -> some View {
        let busy = running.contains(LonghornRequest.key(namespace: node.namespace, name: node.name, node: true))
        let actions = busy ? [] : node.actions
        let act = { (action: LonghornAction) in
            if action == .evict { confirmEvict = node } else { Task { await run(LonghornRequest(node: node, action: action)) } }
        }
        return LonghornNodeRow(node: node, busy: busy)
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
