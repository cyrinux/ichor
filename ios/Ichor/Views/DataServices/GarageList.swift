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
    @State private var removal: RoleRemoval?
    @State private var editing: RoleEdit?
    @State private var resultMessage: String?

    var body: some View {
        List {
            if !status.error.isEmpty { Section { ErrorLine(error: status.error) } }
            if status.hasOperator {
                OperatorSection(status: status)
            }
            ForEach(status.instances) { instance in
                // The operator declares the layout of the clusters it drives: the app shows it, the GarageNode objects change it.
                let managed = status.clusters.contains { $0.instance == instance.label }
                InstanceSection(
                    instance: instance, setting: setting, managed: managed,
                    onTranquility: { confirm = $0 },
                    onEditRole: { editing = RoleEdit(instance: instance, nodeID: $0.nodeID) },
                    onRemoveRole: { removal = RoleRemoval(instance: instance, node: $0) })
            }
        }
        .refreshable { await refresh() }
        .themedBackground()
        .confirmationDialog(
            confirm.map(\.title) ?? "",
            isPresented: $confirm.isPresent(),
            titleVisibility: .visible,
            presenting: confirm
        ) { request in
            Button {
                Task { await setTranquility(request) }
            } label: {
                if request.fullSpeed { Text("Resync at full speed") } else { Text("Default resync speed") }
            }
            Button("Cancel", role: .cancel) {}
        } message: { request in
            if request.fullSpeed {
                Text(
                    "The node resyncs missing blocks without pausing between them: faster recovery, but more disk and network IO while it catches up."
                )
            } else {
                Text("The node goes back to Garage's default resync pace, lighter on disk and network IO.")
            }
        }
        .confirmationDialog(
            removal.map { Text("Remove \($0.node.label) from the layout?") } ?? Text(""),
            isPresented: $removal.isPresent(), titleVisibility: .visible, presenting: removal
        ) { request in
            Button("Remove from layout", role: .destructive) { Task { await stageRemoval(request) } }
            Button("Cancel", role: .cancel) {}
        } message: { _ in
            Text(
                "The removal is staged only: once the layout is applied, the node's data moves to the other nodes. You can revert it before then."
            )
        }
        .sheet(item: $editing) { edit in
            GarageRoleSheet(instance: edit.instance, nodeID: edit.nodeID, known: nil) { _ in
                resultMessage = String(localized: "Role staged. Open the layout to apply it.")
            }
        }
        .messageAlert($resultMessage)
    }

    private func stageRemoval(_ request: RoleRemoval) async {
        guard let client = model.client else { return }
        do {
            _ = try await client.garageStageRemoval(request.instance, node: request.node.nodeID)
            resultMessage = String(localized: "\(request.node.label): removal staged. Open the layout to apply it.")
        } catch {
            resultMessage = String(localized: "Could not change the layout: \(error.localizedDescription)")
        }
    }

    private func setTranquility(_ request: TranquilityRequest) async {
        guard let client = model.client, !setting.contains(request.id) else { return }
        setting.insert(request.id)
        defer { setting.remove(request.id) }
        do {
            try await client.garageSetTranquility(request.instance, node: request.node.nodeID, value: request.value)
            resultMessage =
                request.fullSpeed
                ? String(localized: "\(request.node.label) now resyncs at full speed")
                : String(localized: "\(request.node.label) is back to the default resync speed")
            await refresh()
        } catch {
            resultMessage = String(
                localized: "Could not change the resync speed of \(request.node.label): \(error.localizedDescription)")
        }
    }
}

/// The garage-operator's declarative layer: its own health, then the clusters, buckets, keys
/// and node declarations it manages, problems first. A cluster or bucket opens its object
/// summary.
private struct OperatorSection: View {
    let status: GarageStatus

    private static let group = "garage.rajsingh.info"
    private static let v1 = "v1beta1"

    var body: some View {
        Section {
            if let op = status.`operator` {
                VStack(alignment: .leading, spacing: 3) {
                    HStack {
                        HealthDot(health: op.health)
                        Text(verbatim: op.label).font(.subheadline.monospaced()).lineLimit(1)
                        Spacer()
                        Text(String(localized: "\(op.ready)/\(op.desired) ready")).font(.caption).foregroundStyle(
                            .secondary)
                    }
                    if !op.reasons.isEmpty {
                        Text(verbatim: op.reasons.map(reasonText).joined(separator: " · "))
                            .font(.caption)
                            .foregroundStyle(op.health.needsAttention ? op.health.color : .secondary)
                    }
                }
                .accessibilityElement(children: .combine)
            }
            ForEach(status.clusters) { c in
                NavigationLink {
                    KubeObjectView(
                        resource: KubeAPIResource(
                            group: Self.group, version: status.version, resource: "garageclusters",
                            kind: "GarageCluster"), namespace: c.namespace, name: c.name)
                } label: {
                    OperatorRow(
                        label: c.label, phase: c.phase, health: c.health, details: clusterDetails(c),
                        problems: c.reasons.map(reasonText) + c.conditions.map(\.message))
                }
            }
            ForEach(status.buckets) { b in
                NavigationLink {
                    KubeObjectView(
                        resource: KubeAPIResource(
                            group: Self.group, version: Self.v1, resource: "garagebuckets", kind: "GarageBucket"),
                        namespace: b.namespace, name: b.name)
                } label: {
                    OperatorRow(
                        label: b.label, phase: b.phase, health: b.health, details: bucketDetails(b),
                        problems: b.reasons.map(reasonText) + b.conditions.map(\.message))
                }
            }
            ForEach(status.keys) { k in
                OperatorRow(
                    label: k.label, phase: k.phase, health: k.health, details: keyDetails(k),
                    problems: k.reasons.map(reasonText))
            }
            ForEach(status.nodes) { n in
                OperatorRow(
                    label: n.label, phase: n.phase, health: n.health, details: nodeDetails(n),
                    problems: n.reasons.map(reasonText))
            }
            if status.truncated {
                Label {
                    Text("Some entries are not shown: the cluster has more of a kind than the app lists.")
                } icon: {
                    Image(systemName: "info.circle")
                }
                .font(.caption)
                .foregroundStyle(.secondary)
            }
        } header: {
            Text("Garage operator").textCase(nil)
        }
    }

    private func clusterDetails(_ c: GarageClusterStatus) -> String {
        var parts: [String] = []
        if c.storageReplicas > 0 || c.gatewayReplicas > 0 {
            parts.append(
                String(
                    localized:
                        "Storage \(c.storageReadyReplicas)/\(c.storageReplicas) · gateways \(c.gatewayReadyReplicas)/\(c.gatewayReplicas)"
                ))
        }
        if !c.garageVersion.isEmpty { parts.append(String(localized: "Version") + " " + c.garageVersion) }
        parts.append(String(localized: "Layout v\(c.layoutVersion)"))
        return parts.joined(separator: " · ")
    }

    private func bucketDetails(_ b: GarageBucketStatus) -> String {
        var parts: [String] = []
        if !b.size.isEmpty { parts.append(b.size) }
        if let q = b.quotaUsage {
            parts.append(
                q.sizeLimit > 0
                    ? String(localized: "\(formatBytes(q.sizeBytes)) of \(formatBytes(q.sizeLimit))")
                    : formatBytes(q.sizeBytes))
            if q.objectCount > 0 { parts.append(String(localized: "\(q.objectCount.formatted()) objects")) }
        }
        if !b.websiteURL.isEmpty { parts.append(b.websiteURL) }
        if b.keysWithAccess > 0 { parts.append(String(localized: "\(b.keysWithAccess) keys with access")) }
        return parts.joined(separator: " · ")
    }

    private func keyDetails(_ k: GarageKeyStatus) -> String {
        var parts: [String] = []
        if !k.keyID.isEmpty { parts.append(k.keyID) }
        var grants: [String] = []
        if let all = k.allBuckets {
            grants.append(String(localized: "All buckets:") + " " + permissionWords(all.read, all.write, all.owner))
        }
        for p in k.bucketPermissions.prefix(3) {
            grants.append("\(p.bucket) " + permissionWords(p.read, p.write, p.owner))
        }
        let more = k.bucketPermissionsTotal - min(3, k.bucketPermissions.count)
        if more > 0 { grants.append(String(localized: "+\(more) more")) }
        if !grants.isEmpty { parts.append(grants.joined(separator: " · ")) }
        return parts.joined(separator: " · ")
    }

    private func nodeDetails(_ n: GarageNodeCr) -> String {
        var parts: [String] = []
        if !n.zone.isEmpty { parts.append(String(localized: "zone \(n.zone)")) }
        if !n.capacity.isEmpty { parts.append(n.capacity) }
        if n.gateway { parts.append(String(localized: "gateway")) }
        if !n.connected {
            parts.append(
                n.lastSeenSecs >= 0
                    ? String(localized: "not connected, last seen \(localizedDuration(n.lastSeenSecs)) ago")
                    : String(localized: "not connected"))
        }
        if !n.inLayout { parts.append(String(localized: "not in layout")) }
        return parts.joined(separator: " · ")
    }

    private func permissionWords(_ read: Bool, _ write: Bool, _ owner: Bool) -> String {
        var words: [String] = []
        if read { words.append(String(localized: "read")) }
        if write { words.append(String(localized: "write")) }
        if owner { words.append(String(localized: "owner")) }
        return words.isEmpty ? "—" : words.joined(separator: "+")
    }

    private func reasonText(_ reason: GarageOpReason) -> String {
        switch reason {
        case .clusterFailed: String(localized: "the operator reports it failed")
        case .adminUnreachable: String(localized: "the external cluster's admin API cannot be reached")
        case .operatorDown: String(localized: "the operator is not ready")
        case .degraded: String(localized: "degraded")
        case .storageReplicas: String(localized: "storage replicas missing")
        case .quorumAtRisk: String(localized: "partitions without write quorum")
        case .layoutStaged: String(localized: "a layout change waits to be applied")
        case .tombstones: String(localized: "stale gateway entries to clean up")
        case .nodeDisconnected: String(localized: "a declared node is not connected")
        case .nodeNotInLayout: String(localized: "a declared node has no layout role")
        case .bucketFailed: String(localized: "the operator reports it failed")
        case .lifecycleNotApplied: String(localized: "lifecycle rules were dropped (Garage too old)")
        case .lookupStuck: String(localized: "lookups keep timing out")
        case .metadataDegraded: String(localized: "Garage cannot read its metadata")
        case .deletionBlocked: String(localized: "deletion is held to protect the content")
        case .websiteExposure: String(localized: "the website route is not ready")
        case .keyFailed: String(localized: "the operator reports it failed")
        }
    }
}

/// One garage-operator item: health dot, name, phase, its details and why it is not ok.
private struct OperatorRow: View {
    let label: String
    let phase: String
    let health: ServiceHealth
    let details: String
    let problems: [String]

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack {
                HealthDot(health: health)
                Text(verbatim: label).font(.subheadline.monospaced()).lineLimit(1)
                Spacer()
                if !phase.isEmpty { Text(verbatim: phase).font(.caption).foregroundStyle(.secondary) }
            }
            if !details.isEmpty {
                Text(verbatim: details).font(.caption).foregroundStyle(.secondary)
            }
            if !problems.isEmpty {
                Text(verbatim: problems.joined(separator: " · "))
                    .font(.caption)
                    .foregroundStyle(health.needsAttention ? health.color : .secondary)
            }
        }
        .accessibilityElement(children: .combine)
    }
}

/// A node's removal from the layout waiting for confirmation.
private struct RoleRemoval: Identifiable {
    let instance: GarageInstance
    let node: GarageNode

    var id: String { "\(instance.id)|\(node.id)" }
}

/// The role editor opened on one node.
private struct RoleEdit: Identifiable {
    let instance: GarageInstance
    let nodeID: String

    var id: String { "\(instance.id)|\(nodeID)" }
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
        value = Int(
            node.tranquility == GarageTranquility.fullSpeed ? GarageTranquility.standard : GarageTranquility.fullSpeed)
    }
}

/// One Garage cluster: its state as the header's pill, three tiles (storage nodes up, partitions
/// fully replicated, partitions with write quorum), the resync backlog only while blocks fail to
/// resync, the counters behind "Details", then its nodes with the resync speed in each row's
/// context menu and swipe actions.
private struct InstanceSection: View {
    let instance: GarageInstance
    let setting: Set<String>
    /// The garage-operator declares the layout: no role is edited from here.
    let managed: Bool
    let onTranquility: (TranquilityRequest) -> Void
    let onEditRole: (GarageNode) -> Void
    let onRemoveRole: (GarageNode) -> Void

    /// Actions run Garage's CLI in a ready pod, which only a full (cli-json) reading names.
    private var actionable: Bool { instance.detailed && !instance.pod.isEmpty }

    var body: some View {
        Section {
            if !instance.detailed {
                Label {
                    Text("Only the basic status is available: \(instance.message.or("—"))")
                } icon: {
                    Image(systemName: "info.circle")
                }
                .font(.caption)
                .foregroundStyle(.secondary)
            }
            if instance.storageNodes > 0 || instance.partitions > 0 {
                HStack(alignment: .top, spacing: 12) {
                    if instance.storageNodes > 0 {
                        GarageTile(
                            count: instance.storageNodesUp, of: instance.storageNodes,
                            label: String(localized: "Storage nodes"), shortColor: .statusWarn)
                    }
                    if instance.partitions > 0 {
                        GarageTile(
                            count: instance.partitionsAllOk, of: instance.partitions,
                            label: String(localized: "Replicated partitions"), shortColor: .statusWarn)
                        GarageTile(
                            count: instance.partitionsQuorum, of: instance.partitions,
                            label: String(localized: "Write quorum"), shortColor: .statusBad)
                    }
                }
            }
            if instance.resyncErrors > 0 {
                Text(
                    verbatim: String(
                        localized:
                            "Resync queue \(max(0, instance.resyncQueue).formatted()) · failing \(instance.resyncErrors.formatted())"
                    )
                )
                .font(.callout)
                .monospacedDigit()
                .foregroundStyle(.statusBad)
            }
            DisclosureGroup {
                if instance.storageNodes > 0 {
                    LabeledContent(
                        "Storage nodes",
                        value: String(localized: "\(instance.storageNodesUp)/\(instance.storageNodes) up"))
                }
                if instance.partitions > 0 {
                    LabeledContent("Fully replicated", value: "\(instance.partitionsAllOk)/\(instance.partitions)")
                    LabeledContent("Write quorum", value: "\(instance.partitionsQuorum)/\(instance.partitions)")
                }
                if instance.resyncQueue >= 0 { LabeledContent("Resync queue", value: instance.resyncQueue.formatted()) }
                if instance.resyncErrors >= 0 {
                    LabeledContent("Blocks failing to resync") {
                        Text(verbatim: instance.resyncErrors.formatted()).foregroundStyle(
                            instance.resyncErrors > 0 ? Color.statusBad : Color.secondary)
                    }
                }
                if instance.tableSyncQueue >= 0 {
                    LabeledContent("Metadata sync queue", value: instance.tableSyncQueue.formatted())
                }
                LabeledContent("Pods", value: String(localized: "\(instance.podsReady)/\(instance.pods) ready"))
                if !instance.version.isEmpty { LabeledContent("Version", value: instance.version) }
            } label: {
                Text("Details")
            }
            if actionable {
                NavigationLink {
                    GarageBlockErrorsView(instance: instance)
                } label: {
                    Label("Block errors", systemImage: "cube.transparent")
                }
                let members = instance.nodes.filter(\.storage).count
                NavigationLink {
                    GarageLayoutView(instance: instance, managed: managed)
                } label: {
                    Label {
                        VStack(alignment: .leading, spacing: 2) {
                            Text("Layout v\(instance.layoutVersion)")
                            Text("\(members) nodes in the layout").font(.caption).foregroundStyle(.secondary)
                        }
                    } icon: {
                        Image(systemName: "square.grid.3x3")
                    }
                }
            }
            ForEach(instance.nodes) { node in
                let request = TranquilityRequest(instance: instance, node: node)
                let busy = request.map { setting.contains($0.id) } ?? false
                // A layout role is given to a connected node, changed or taken from a member; the operator's clusters keep theirs.
                let editable = actionable && !managed && node.nodeID.count == 64 && (node.storage || node.up)
                NodeRow(node: node, busy: busy)
                    .swipeActions(edge: .trailing) {
                        if let request, !busy {
                            Button {
                                onTranquility(request)
                            } label: {
                                TranquilityLabel(fullSpeed: request.fullSpeed)
                            }
                            .tint(request.fullSpeed ? .orange : .blue)
                        }
                    }
                    .contextMenu {
                        if let request, !busy {
                            Button {
                                onTranquility(request)
                            } label: {
                                TranquilityLabel(fullSpeed: request.fullSpeed)
                            }
                        }
                        if editable {
                            Button {
                                onEditRole(node)
                            } label: {
                                if node.storage {
                                    Label("Change role…", systemImage: "slider.horizontal.3")
                                } else {
                                    Label("Add to layout…", systemImage: "plus.circle")
                                }
                            }
                            if node.storage {
                                Button(role: .destructive) {
                                    onRemoveRole(node)
                                } label: {
                                    Label("Remove from layout", systemImage: "minus.circle")
                                }
                            }
                        }
                    }
            }
        } header: {
            SectionHeader(
                title: instance.label, pill: StatusPill(label: instance.state.label, color: instance.state.health.color)
            )
            .textCase(nil)
        }
    }
}

/// "6 / 7" over what it counts, with the share as a bar: green when all are there, else
/// `shortColor`. A plain bar, not a UsageBar: here a full bar is the good case.
private struct GarageTile: View {
    let count: Int
    let of: Int
    let label: String
    let shortColor: Color

    var body: some View {
        StatTile(
            icon: nil, value: "\(count) / \(of)", label: label,
            meter: {
                AnyView(
                    ProgressView(value: Double(min(max(count, 0), of)), total: Double(max(of, 1)))
                        .tint(count >= of ? Color.statusOK : shortColor))
            }
        )
        .frame(maxWidth: .infinity, alignment: .leading)
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

/// A Garage node: where it runs, its data disk as a meter, and its resync backlog as a tag.
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
        ResourceRow(health: health, title: node.label, subtitle: details, trailing: trailing) {
            if node.dataTotal > 0 {
                let used = node.dataTotal - node.dataAvail
                MeterRow(
                    label: String(localized: "data"), fraction: Double(used) / Double(node.dataTotal),
                    value: "\(formatBytes(used)) / \(formatBytes(node.dataTotal))")
            }
        }
        .accessibilityElement(children: .combine)
    }

    @ViewBuilder private var trailing: some View {
        if busy {
            ProgressView()
        } else if node.resyncQueue > 0 {
            Tag(String(localized: "resync \(node.resyncQueue.formatted())"), color: .statusWarn)
        }
    }

    /// "on <host> · zone <zone>", then why it is down or not counted, and its resync speed.
    private var details: String {
        var parts: [String] = []
        if !node.up {
            parts.append(
                node.lastSeenSecs >= 0
                    ? String(localized: "down, last seen \(localizedDuration(node.lastSeenSecs)) ago")
                    : String(localized: "down"))
        }
        if !node.kubeNode.isEmpty { parts.append(String(localized: "on \(node.kubeNode)")) }
        if !node.zone.isEmpty { parts.append(String(localized: "zone \(node.zone)")) }
        if !node.storage { parts.append(String(localized: "not in layout")) }
        if node.resyncErrors > 0 {
            parts.append(String(localized: "Blocks failing to resync") + " " + node.resyncErrors.formatted())
        }
        if !node.statsError.isEmpty && node.up { parts.append(String(localized: "no statistics: \(node.statsError)")) }
        if node.tranquility == GarageTranquility.fullSpeed {
            parts.append(String(localized: "resync: full speed"))
        } else if node.tranquility > 0 {
            parts.append(String(localized: "resync tranquility \(node.tranquility)"))
        }
        return parts.joined(separator: " · ")
    }
}
