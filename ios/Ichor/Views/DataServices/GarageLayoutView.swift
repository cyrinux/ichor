import SwiftUI
import IchorCore

/// A Garage cluster's layout: version, partition size and usable capacity as tiles, the staged
/// changes with what applying them would move (apply with the new version typed, or revert),
/// the roles with how much of each capacity the layout uses, the nodes without a role (they can
/// join), and the version history. When the garage-operator declares the layout (`managed`) it
/// is shown, not edited from here (os:admin).
struct GarageLayoutView: View {
    let instance: GarageInstance
    /// The garage-operator declares this cluster's layout: the GarageNode objects change it.
    let managed: Bool

    @Environment(AppModel.self) private var model
    @State private var state: LoadState<GarageLayout> = .loading
    @State private var busy = false
    @State private var confirmApply = false
    @State private var confirmRevert = false
    @State private var removal: GarageLayoutNode?
    @State private var editing: GarageRoleTarget?
    @State private var redundancy = false
    @State private var skipChoice = false
    @State private var skip: SkipRequest?
    @State private var resultMessage: String?

    private var layout: GarageLayout? {
        if case .loaded(let layout, _, _) = state { return layout }
        return nil
    }
    private var editable: Bool { !managed && !busy }

    var body: some View {
        LoadStateView(state: state, retry: load) { layout in
            List {
                SummarySection(layout: layout, onRedundancy: editable ? { redundancy = true } : nil)
                if managed {
                    Section {
                        Label(
                            "The garage-operator declares this layout: change the GarageNode objects, not the layout here.",
                            systemImage: "info.circle"
                        )
                        .font(.callout)
                        .foregroundStyle(.secondary)
                    }
                }
                if layout.hasStaged { stagedSection(layout) }
                rolesSection(layout)
                if !layout.candidates.isEmpty { candidatesSection(layout) }
                HistorySection(layout: layout, onSkip: editable && layout.stuck ? { skipChoice = true } : nil)
            }
            .refreshable { await load() }
            .themedBackground()
        }
        .task { await load() }
        .navigationTitle(Text("Cluster layout"))
        .navigationBarTitleDisplayMode(.inline)
        .sheet(isPresented: $confirmApply) {
            if let layout, layout.hasStaged {
                HostnameConfirmationSheet(
                    title: String(localized: "Apply layout version \(layout.nextVersion)?"),
                    message: applyMessage(layout),
                    hostname: String(layout.nextVersion),
                    actionTitle: String(localized: "Apply"),
                    onConfirm: {
                        Task {
                            await write(
                                String(
                                    localized:
                                        "Layout version \(layout.nextVersion) applied: data now moves between nodes.")
                            ) {
                                try await $0.garageApplyLayout(instance, version: layout.nextVersion)
                            }
                        }
                    }
                )
            }
        }
        .sheet(item: $editing) { target in
            GarageRoleSheet(instance: instance, nodeID: target.id, known: layout) { staged in
                state = state.refreshed(with: .loaded(staged, at: Date()))
                resultMessage = String(localized: "Role staged. Apply the layout to enact it.")
            }
        }
        .sheet(isPresented: $redundancy) {
            if let layout {
                ZoneRedundancySheet(
                    current: layout.stagedZoneRedundancy.isEmpty ? layout.zoneRedundancy : layout.stagedZoneRedundancy
                ) { value in
                    Task {
                        await write(String(localized: "Zone redundancy staged. Apply the layout to enact it.")) {
                            try await $0.garageSetZoneRedundancy(instance, value: value)
                        }
                    }
                }
                .presentationDetents([.medium])
            }
        }
        .confirmationDialog(Text("Drop the staged changes?"), isPresented: $confirmRevert, titleVisibility: .visible) {
            Button("Revert", role: .destructive) {
                Task {
                    await write(String(localized: "Staged changes dropped.")) {
                        try await $0.garageRevertLayout(instance)
                    }
                }
            }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("Every staged role change and parameter goes away; the current layout stays as it is.")
        }
        .confirmationDialog(
            removal.map { Text("Remove \($0.label) from the layout?") } ?? Text(""),
            isPresented: $removal.isPresent(), titleVisibility: .visible, presenting: removal
        ) { node in
            Button("Remove from layout", role: .destructive) {
                Task {
                    await write(String(localized: "\(node.label): removal staged. Apply the layout to enact it.")) {
                        try await $0.garageStageRemoval(instance, node: node.id)
                    }
                }
            }
            Button("Cancel", role: .cancel) {}
        } message: { _ in
            Text(
                "The removal is staged only: once the layout is applied, the node's data moves to the other nodes. You can revert it before then."
            )
        }
        .confirmationDialog(Text("Skip the dead nodes?"), isPresented: $skipChoice, titleVisibility: .visible) {
            Button("Skip dead nodes") { skip = SkipRequest(allowMissingData: false) }
            Button("Skip and allow missing data", role: .destructive) { skip = SkipRequest(allowMissingData: true) }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text(
                "The draining layout stops waiting on the nodes that are down: Garage marks them as having acknowledged the current version, and the migration goes on without them. Allowing missing data also moves every node's sync tracker: metadata that only the dead nodes held is given up for good. There is no undo; use it only for nodes that will not come back."
            )
        }
        .sheet(item: $skip) { request in
            if let layout {
                HostnameConfirmationSheet(
                    title: String(localized: "Skip the dead nodes of \(instance.name)?"),
                    message: skipMessage(layout),
                    hostname: instance.name,
                    actionTitle: String(localized: "Skip dead nodes"),
                    acknowledgments: request.allowMissingData
                        ? [String(localized: "Metadata that only the dead nodes held is given up for good.")] : [],
                    onConfirm: {
                        skip = nil
                        Task {
                            await write(
                                String(localized: "Dead nodes skipped: the layout migration goes on without them.")
                            ) {
                                try await $0.garageSkipDeadNodes(
                                    instance, version: layout.version, allowMissingData: request.allowMissingData)
                            }
                        }
                    }
                )
            }
        }
        .messageAlert($resultMessage)
    }

    private func skipMessage(_ layout: GarageLayout) -> String {
        let down = layout.trackers.filter { t in layout.node(t.id).map { !$0.up } ?? false }.map(\.label).joined(
            separator: ", ")
        return String(
            localized:
                "The draining layout stops waiting on \(down.isEmpty ? "—" : down): Garage marks them as having acknowledged version \(layout.version), and the migration goes on without them. There is no undo; use it only for nodes that will not come back."
        )
    }

    private func applyMessage(_ layout: GarageLayout) -> String {
        if let moved = layout.preview?.movedPartitions, moved >= 0 {
            return String(
                localized:
                    "The whole cluster moves to the new layout: \(moved) partitions change nodes, adding disk and network load until the rebalance is done. Garage refuses the apply if another change was staged meanwhile."
            )
        }
        return String(
            localized:
                "The whole cluster moves to the new layout and data is rebalanced between nodes, adding disk and network load until it is done. Garage refuses the apply if another change was staged meanwhile."
        )
    }

    @ViewBuilder private func stagedSection(_ layout: GarageLayout) -> some View {
        Section {
            if !layout.stagedZoneRedundancy.isEmpty {
                LabeledContent("Zone redundancy", value: redundancyLabel(layout.stagedZoneRedundancy))
            }
            ForEach(layout.staged) { StagedRow(node: $0) }
            if let preview = layout.preview { PreviewRows(preview: preview) }
            HStack {
                Button("Revert", role: .destructive) { confirmRevert = true }
                    .buttonStyle(.bordered)
                    .disabled(!editable)
                Spacer()
                if busy { ProgressView() }
                Button(String(localized: "Apply version \(layout.nextVersion)")) { confirmApply = true }
                    .buttonStyle(.borderedProminent)
                    .disabled(!editable || layout.preview?.ok == false)
            }
        } header: {
            Text("Staged for version \(layout.nextVersion)").textCase(nil)
        }
    }

    @ViewBuilder private func rolesSection(_ layout: GarageLayout) -> some View {
        Section {
            if layout.members.isEmpty {
                Text("No node has a role yet: give one to a node below.").font(.callout).foregroundStyle(.secondary)
            }
            ForEach(layout.members) { node in
                MemberRow(node: node)
                    .contextMenu {
                        if editable {
                            Button {
                                editing = GarageRoleTarget(id: node.id)
                            } label: {
                                Label("Change role…", systemImage: "slider.horizontal.3")
                            }
                            if node.staged != .remove {
                                Button(role: .destructive) {
                                    removal = node
                                } label: {
                                    Label("Remove from layout", systemImage: "minus.circle")
                                }
                            }
                        }
                    }
                    .swipeActions(edge: .trailing) {
                        if editable, node.staged != .remove {
                            Button {
                                removal = node
                            } label: {
                                Label("Remove from layout", systemImage: "minus.circle")
                            }.tint(.orange)
                        }
                    }
            }
        } header: {
            Text("Roles").textCase(nil)
        }
    }

    @ViewBuilder private func candidatesSection(_ layout: GarageLayout) -> some View {
        Section {
            ForEach(layout.candidates) { node in
                ResourceRow(
                    health: node.up ? .idle : .warning, title: node.label, subtitle: candidateDetails(node),
                    trailing: Group {
                        if editable {
                            Button("Add to layout…") { editing = GarageRoleTarget(id: node.id) }.buttonStyle(
                                .borderless)
                        }
                    }
                ) {}
            }
        } header: {
            Text("Nodes without a role").textCase(nil)
        }
    }

    private func candidateDetails(_ node: GarageLayoutNode) -> String? {
        var parts: [String] = []
        if !node.kubeNode.isEmpty { parts.append(String(localized: "on \(node.kubeNode)")) }
        if node.dataTotal > 0 { parts.append(String(localized: "disk \(formatBytes(node.dataTotal))")) }
        if !node.up { parts.append(String(localized: "down")) }
        return parts.isEmpty ? nil : parts.joined(separator: " · ")
    }

    private func load() async {
        guard let client = model.client else { return }
        let loaded: LoadState<GarageLayout> = await .from { try await client.garageLayout(instance) }
        state = state.refreshed(with: loaded)
    }

    /// One write on the layout; the layout it answers replaces the one shown.
    private func write(_ done: String, _ action: @escaping (TalosClient) async throws -> GarageLayout) async {
        guard let client = model.client, !busy else { return }
        busy = true
        defer { busy = false }
        do {
            let layout = try await action(client)
            state = state.refreshed(with: .loaded(layout, at: Date()))
            resultMessage = ([done] + layout.messages).joined(separator: "\n")
        } catch {
            resultMessage = String(localized: "Could not change the layout: \(error.localizedDescription)")
        }
    }
}

/// The node whose role the editor sheet is opened on.
struct GarageRoleTarget: Identifiable {
    let id: String
}

/// A skip of the dead nodes waiting for its typed confirmation.
private struct SkipRequest: Identifiable {
    let allowMissingData: Bool

    var id: Bool { allowMissingData }
}

/// "maximum" or "2 zones".
func redundancyLabel(_ value: String) -> String {
    if let zones = Int(value) { return String(localized: "\(zones) zones") }
    return String(localized: "maximum")
}

/// Capacity or gateway, then the zone and tags: "450 GiB · zone zone-1 · demo-worker-1".
func garageRoleLabel(_ role: GarageLayoutRole) -> String {
    var parts = [
        role.gateway ? String(localized: "gateway") : formatBytes(role.capacity),
        String(localized: "zone \(role.zone)"),
    ]
    parts.append(contentsOf: role.tags)
    return parts.joined(separator: " · ")
}

private struct SummarySection: View {
    let layout: GarageLayout
    let onRedundancy: (() -> Void)?

    var body: some View {
        Section {
            HStack(alignment: .top, spacing: 12) {
                StatTile(icon: nil, value: "v\(layout.version)", label: String(localized: "Layout version"))
                StatTile(
                    icon: nil, value: layout.partitionSize > 0 ? formatBytes(layout.partitionSize) : "—",
                    label: String(localized: "Partition size"))
                let usable = layout.members.reduce(Int64(0)) { $0 + ($1.role?.usableCapacity ?? 0) }
                StatTile(
                    icon: nil, value: usable > 0 ? formatBytes(usable) : "—",
                    label: String(localized: "Usable capacity"))
            }
            LabeledContent("Storage nodes", value: "\(layout.storageNodes)")
            if layout.gatewayNodes > 0 { LabeledContent("Gateway nodes", value: "\(layout.gatewayNodes)") }
            LabeledContent("Zone redundancy") {
                HStack {
                    Text(verbatim: redundancyLabel(layout.zoneRedundancy))
                    if let onRedundancy {
                        Button("Change", action: onRedundancy).buttonStyle(.borderless)
                    }
                }
            }
        }
    }
}

/// A staged change: who joins (ok), changes (warn) or leaves (bad), and the role after it.
private struct StagedRow: View {
    let node: GarageLayoutNode

    var body: some View {
        let (health, tag, color): (ServiceHealth, String, Color) =
            switch node.staged {
            case .add: (.ok, String(localized: "joins"), .statusOK)
            case .remove: (.critical, String(localized: "leaves"), .statusBad)
            case .change, .unchanged: (.warning, String(localized: "changes"), .statusWarn)
            }
        ResourceRow(health: health, title: node.label, subtitle: subtitle, trailing: Tag(tag, color: color)) {}
    }

    private var subtitle: String? {
        switch node.staged {
        case .remove: node.role.map { String(localized: "was \(garageRoleLabel($0))") }
        case .change:
            [node.stagedRole.map(garageRoleLabel), node.role.map { String(localized: "was \(garageRoleLabel($0))") }]
                .compactMap { $0 }.joined(separator: " · ")
        case .add, .unchanged: node.stagedRole.map(garageRoleLabel)
        }
    }
}

/// What applying would do, in Garage's own words, or why it cannot be computed.
private struct PreviewRows: View {
    let preview: GarageLayoutPreview

    var body: some View {
        if !preview.error.isEmpty {
            Text("Garage cannot compute this layout: \(preview.error)").font(.callout).foregroundStyle(.statusBad)
        } else {
            if preview.movedPartitions >= 0 {
                LabeledContent("Partitions moved", value: preview.movedPartitions.formatted())
            }
            if preview.usableCapacity > 0 {
                LabeledContent("Usable capacity after", value: formatBytes(preview.usableCapacity))
            }
            if preview.lowUsableCapacity {
                Text("Part of the declared capacity cannot be used: zones or nodes are too unequal.").font(.caption)
                    .foregroundStyle(.statusWarn)
            }
            if preview.lowPartitionSize {
                Text("Partitions would be very small: Garage recommends more capacity per node.").font(.caption)
                    .foregroundStyle(.statusWarn)
            }
            ForEach(Array(preview.messages.enumerated()), id: \.offset) { _, line in
                Text(verbatim: line).font(.caption).foregroundStyle(.secondary)
            }
        }
    }
}

/// A node with a role: its state, the role, and the share of its capacity the layout uses.
private struct MemberRow: View {
    let node: GarageLayoutNode

    private var health: ServiceHealth {
        if !node.known { return .idle }
        if !node.up { return .critical }
        if node.draining { return .warning }
        return .ok
    }

    var body: some View {
        ResourceRow(health: health, title: node.label, subtitle: details, trailing: trailing) {
            if let role = node.role, !role.gateway, role.usableCapacity > 0 {
                // Here a full bar is the good case: the whole capacity is usable.
                MeterRow(
                    label: String(localized: "usable"), fraction: role.usableFraction,
                    value: "\(formatBytes(role.usableCapacity)) / \(formatBytes(role.capacity))", warnAt: 2)
            }
        }
        .accessibilityElement(children: .combine)
    }

    @ViewBuilder private var trailing: some View {
        switch node.staged {
        case .remove: Tag(String(localized: "leaves"), color: .statusBad)
        case .change: Tag(String(localized: "changes"), color: .statusWarn)
        case .add, .unchanged: EmptyView()
        }
    }

    private var details: String {
        var parts: [String] = []
        if let role = node.role { parts.append(garageRoleLabel(role)) }
        if !node.kubeNode.isEmpty { parts.append(String(localized: "on \(node.kubeNode)")) }
        if node.known && !node.up { parts.append(String(localized: "down")) }
        if !node.known { parts.append(String(localized: "no longer known to the cluster")) }
        if let partitions = node.role?.storedPartitions, partitions > 0 {
            parts.append(String(localized: "\(partitions) partitions"))
        }
        return parts.joined(separator: " · ")
    }
}

/// The versions, newest first, behind a disclosure; the trackers while several are live, and,
/// when a draining version waits on a node that is down, the way past it (`onSkip`).
private struct HistorySection: View {
    let layout: GarageLayout
    let onSkip: (() -> Void)?

    @State private var open = false

    var body: some View {
        Section {
            DisclosureGroup(isExpanded: $open) {
                if layout.stuck {
                    let down = layout.trackers.filter { t in layout.node(t.id).map { !$0.up } ?? false }.map(\.label)
                        .joined(separator: ", ")
                    let names = down.isEmpty ? "—" : down
                    Text(
                        "A draining version waits on a node that is down (\(names)). Bring it back, or skip it with garage layout skip-dead-nodes."
                    )
                    .font(.caption)
                    .foregroundStyle(.statusBad)
                    if let onSkip {
                        Button("Skip dead nodes…", role: .destructive, action: onSkip)
                    }
                }
                ForEach(layout.versions) { version in
                    HStack {
                        Text(verbatim: "v\(version.version)").font(.subheadline.monospaced())
                        Text("\(version.storageNodes) storage · \(version.gatewayNodes) gateways").font(.caption)
                            .foregroundStyle(.secondary)
                        Spacer()
                        StatusPill(label: statusLabel(version.status), color: statusColor(version.status))
                    }
                }
                if !layout.trackers.isEmpty {
                    LabeledContent("Acknowledged by every node", value: "v\(layout.minAck)")
                    ForEach(layout.trackers) { t in
                        LabeledContent(
                            t.label,
                            value: String(localized: "ack v\(t.ack) · sync v\(t.sync) · sync-ack v\(t.syncAck)")
                        )
                        .font(.caption.monospaced())
                    }
                }
            } label: {
                HStack {
                    Text("History")
                    if layout.stuck { Tag(String(localized: "stuck"), color: .statusBad) }
                }
            }
        }
        .onAppear { if layout.stuck { open = true } }
    }

    private func statusLabel(_ status: GarageLayoutStatus) -> String {
        switch status {
        case .current: String(localized: "current")
        case .draining: String(localized: "draining")
        case .historical: String(localized: "historical")
        case .unknown: String(localized: "Unknown")
        }
    }

    private func statusColor(_ status: GarageLayoutStatus) -> Color {
        switch status {
        case .current: .statusOK
        case .draining: .statusWarn
        case .historical, .unknown: .secondary
        }
    }
}

/// Maximum, or a least number of zones; Garage refuses more than its replication factor.
private struct ZoneRedundancySheet: View {
    let current: String
    let onPick: (String) -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var picked = ""

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Picker("Zone redundancy", selection: $picked) {
                        ForEach(["maximum", "1", "2", "3"], id: \.self) { Text(verbatim: redundancyLabel($0)).tag($0) }
                    }
                    .pickerStyle(.inline)
                    .labelsHidden()
                } footer: {
                    Text(
                        "The least number of zones each partition is replicated in; maximum spreads the replicas over as many zones as there are. Garage refuses more zones than its replication factor."
                    )
                }
            }
            .themedBackground()
            .navigationTitle(Text("Zone redundancy"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Stage change") {
                        onPick(picked)
                        dismiss()
                    }
                    .disabled(picked == current)
                }
            }
            .onAppear { picked = current }
        }
    }
}
