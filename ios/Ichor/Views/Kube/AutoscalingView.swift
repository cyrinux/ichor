import SwiftUI
import IchorCore

/// What scales the cluster (os:admin): the rows that need attention first, then every group's
/// rows — HPAs, KEDA, VPA recommendations, Karpenter pools — each opening its detail sheet.
struct AutoscalingView: View {
    @Environment(AppModel.self) private var model
    @State private var state: LoadState<AutoscalingReport> = .loading
    @State private var namespace: String?
    @State private var query = ""
    @State private var selected: Selection?

    /// What a tap opens: one row of one group, with the claims next to its pool.
    enum Selection: Hashable, Identifiable {
        case hpa(HpaRow)
        case scaledObject(ScaledObjectRow)
        case scaledJob(ScaledJobRow)
        case vpa(VpaRow)
        case pool(NodePoolRow, claims: [NodeClaimRow])
        case nodeGroup(ManagedNodeGroupRow, clusterVersion: String, gke: Bool)
        case controlPlane(EksUpgradePlan)

        var id: String {
            switch self {
            case .hpa(let row): "hpa/" + row.id
            case .scaledObject(let row): "so/" + row.id
            case .scaledJob(let row): "sj/" + row.id
            case .vpa(let row): "vpa/" + row.id
            case .pool(let row, _): "pool/" + row.id
            case .nodeGroup(let row, _, _): "ng/" + row.id
            case .controlPlane: "eks/control-plane"
            }
        }
    }

    var body: some View {
        LoadStateView(state: state, retry: load) { report in
            list(report)
        }
        .task { await load() }
        // A new API address (set on the Kubernetes screen): read again through it.
        .id(model.client?.kubeServer)
        .searchable(text: $query, prompt: Text("Name, target or trigger"))
        .navigationTitle("Autoscaling")
        .navigationBarTitleDisplayMode(.inline)
        .sheet(item: $selected) { selection in
            AutoscalingDetailSheets(selection: live(selection), onChanged: load)
        }
    }

    private func list(_ report: AutoscalingReport) -> some View {
        let namespaces = report.namespaceNames
        let chosen = namespace.flatMap { namespaces.contains($0) ? $0 : nil }
        let view = report.filtered(namespace: chosen, query: query)
        let attention = view.problems
        let claims = Dictionary(grouping: view.karpenter.nodeClaims, by: \.pool)
        return List {
            attentionSection(attention, view: view, claims: claims)
            group("Horizontal (HPA)", state: view.hpa.state) {
                ForEach(view.hpa.items) { row in
                    Button {
                        selected = .hpa(row)
                    } label: {
                        HpaRowView(row: row)
                    }
                }
            }
            group("KEDA", state: view.keda.state) {
                ForEach(view.keda.scaledObjects) { row in
                    Button {
                        selected = .scaledObject(row)
                    } label: {
                        ScaledObjectRowView(row: row)
                    }
                }
                ForEach(view.keda.scaledJobs) { row in
                    Button {
                        selected = .scaledJob(row)
                    } label: {
                        ScaledJobRowView(row: row)
                    }
                }
            }
            group("Vertical (VPA)", state: view.vpa.state) {
                ForEach(view.vpa.items) { row in
                    Button {
                        selected = .vpa(row)
                    } label: {
                        VpaRowView(row: row)
                    }
                }
            }
            group("Node pools (Karpenter)", state: view.karpenter.state) {
                ForEach(view.karpenter.nodePools) { row in
                    Button {
                        selected = .pool(row, claims: claims[row.name] ?? [])
                    } label: {
                        NodePoolRowView(row: row, claimCount: claims[row.name]?.count ?? 0)
                    }
                }
            }
            // Only on a cluster that signs in through EKS or GKE: elsewhere there is nothing to say.
            if view.nodeGroups.installed {
                nodeGroupsSection(view.nodeGroups, plan: view.eksUpgrade)
            }
        }
        .overlay {
            if view.hpa.items.isEmpty && view.keda.scaledObjects.isEmpty && view.keda.scaledJobs.isEmpty
                && view.vpa.items.isEmpty && view.karpenter.nodePools.isEmpty && view.nodeGroups.groups.isEmpty
            {
                ContentUnavailableView("Nothing scales this cluster automatically", systemImage: "arrow.up.and.down")
            } else if attention.isEmpty && allRows(view).isEmpty && !query.isEmpty {
                ContentUnavailableView.search(text: query)
            }
        }
        .refreshable { await load() }
        .themedBackground()
    }

    private func allRows(_ view: AutoscalingReport) -> [Any] {
        view.hpa.items + view.keda.scaledObjects + view.keda.scaledJobs + view.vpa.items + view.karpenter.nodePools
            + view.nodeGroups.groups
    }

    /// The EKS managed node groups or GKE node pools, with the permission or provider error line
    /// above them and the EKS control plane's row first.
    private func nodeGroupsSection(_ report: NodeGroupsReport, plan: EksUpgradePlan) -> some View {
        Section {
            let error = NodeGroupText.error(report)
            if !error.isEmpty {
                Text(AutoscaleText.groupError(error)).font(.footnote).foregroundStyle(Color.statusBad)
            }
            if plan.installed {
                Button {
                    selected = .controlPlane(plan)
                } label: {
                    EksControlPlaneRowView(plan: plan)
                }
            }
            ForEach(report.groups) { row in
                Button {
                    selected = .nodeGroup(row, clusterVersion: report.clusterVersion, gke: report.gke)
                } label: {
                    NodeGroupRowView(row: row)
                }
            }
        } header: {
            Text(verbatim: NodeGroupText.title(gke: report.gke))
        }
    }

    /// The rows that need attention, across the four groups; empty says everything is fine.
    private func attentionSection(
        _ problems: [AutoscalingProblem], view: AutoscalingReport, claims: [String: [NodeClaimRow]]
    ) -> some View {
        Section {
            if problems.isEmpty {
                Text("Everything that scales is within its bounds.").foregroundStyle(.secondary)
            }
            ForEach(problems) { problem in
                Button {
                    select(problem, in: view, claims: claims)
                } label: {
                    ProblemRow(problem: problem, gke: view.nodeGroups.gke)
                }
            }
        } header: {
            Text("Needs attention")
        }
    }

    private func select(_ problem: AutoscalingProblem, in view: AutoscalingReport, claims: [String: [NodeClaimRow]]) {
        switch problem.group {
        case .hpa:
            if let row = view.hpa.items.first(where: { $0.id == problem.key }) { selected = .hpa(row) }
        case .keda:
            if let row = view.keda.scaledObjects.first(where: { $0.id == problem.key }) {
                selected = .scaledObject(row)
            } else if let row = view.keda.scaledJobs.first(where: { $0.id == problem.key }) {
                selected = .scaledJob(row)
            }
        case .vpa:
            if let row = view.vpa.items.first(where: { $0.id == problem.key }) { selected = .vpa(row) }
        case .karpenter:
            if let row = view.karpenter.nodePools.first(where: { $0.id == problem.key }) {
                selected = .pool(row, claims: claims[row.name] ?? [])
            } else if let claim = view.karpenter.nodeClaims.first(where: { $0.id == problem.key }),
                let pool = view.karpenter.nodePools.first(where: { $0.name == claim.pool })
            {
                selected = .pool(pool, claims: claims[pool.name] ?? [])
            }
        case .nodeGroups:
            if let row = view.nodeGroups.groups.first(where: { $0.id == problem.key }) {
                selected = .nodeGroup(
                    row, clusterVersion: view.nodeGroups.clusterVersion, gke: view.nodeGroups.gke
                )
            }
        case .unknown:
            break
        }
    }

    /// One group's section: its rows, or the one line a missing or failed group gets.
    private func group(_ title: String, state: AutoscalingGroupState, @ViewBuilder rows: () -> some View) -> some View {
        Section {
            if state.installed {
                if !state.error.isEmpty {
                    Text(AutoscaleText.groupError(state.error)).font(.footnote).foregroundStyle(Color.statusBad)
                }
                if state.truncated {
                    Text("The list was cut: the rest is not shown.").font(.footnote).foregroundStyle(.secondary)
                }
                rows()
            } else {
                Text(AutoscaleText.notInstalled(title)).foregroundStyle(.secondary)
            }
        } header: {
            Text(title)
        }
    }

    /// The selection with its row as the last read has it: an action's outcome shows on the
    /// open sheet. The selection itself when the row is gone or nothing is loaded.
    private func live(_ selection: Selection) -> Selection {
        guard case .loaded(let report, _, _) = state else { return selection }
        switch selection {
        case .hpa(let row):
            return report.hpa.items.first { $0.id == row.id }.map(Selection.hpa) ?? selection
        case .scaledObject(let row):
            return report.keda.scaledObjects.first { $0.id == row.id }.map(Selection.scaledObject) ?? selection
        case .controlPlane:
            return report.eksUpgrade.installed ? .controlPlane(report.eksUpgrade) : selection
        default:
            return selection
        }
    }

    private func load() async {
        guard let client = model.client else { return }
        state = state.refreshed(with: await .from { try await client.autoscaling() })
    }
}

/// One "Needs attention" row: the group it came from, its name and its state.
private struct ProblemRow: View {
    let problem: AutoscalingProblem
    /// The node pools are GKE's: their group is titled so.
    var gke = false

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 6) {
                Text(verbatim: problem.label)
                    .font(.callout.monospaced())
                    .lineLimit(1)
                    .truncationMode(.middle)
                    .foregroundStyle(.primary)
                Spacer()
                AutoscaleStateBadge(state: problem.state)
            }
            Text(verbatim: groupTitle).font(.caption).foregroundStyle(.secondary).lineLimit(1)
            if !problem.message.isEmpty {
                Text(verbatim: problem.message).font(.caption).foregroundStyle(.secondary).lineLimit(2)
            }
        }
    }

    private var groupTitle: String {
        switch problem.group {
        case .hpa: "Horizontal (HPA)"
        case .keda: "KEDA"
        case .vpa: "Vertical (VPA)"
        case .karpenter: "Node pools (Karpenter)"
        case .nodeGroups: NodeGroupText.title(gke: gke)
        case .unknown: ""
        }
    }
}

/// Name, its target and bounds, and the state.
private struct HpaRowView: View {
    let row: HpaRow

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 6) {
                Text(verbatim: row.name).font(.callout.monospaced()).lineLimit(1).truncationMode(.middle)
                    .foregroundStyle(.primary)
                Spacer()
                AutoscaleStateBadge(state: row.state)
            }
            Text(
                verbatim: [row.namespace, row.target.label, hpaBoundsText(row.current, row.min, row.max)]
                    .filter { !$0.isEmpty }.joined(separator: "  ·  ")
            )
            .font(.caption).foregroundStyle(.secondary).lineLimit(1)
            if !row.managedBy.isEmpty {
                Text(AutoscaleText.managedBy(row.managedBy, row.scaledObject))
                    .font(.caption).foregroundStyle(.secondary).lineLimit(1)
            }
        }
    }
}

/// Name, its target and triggers, and the state.
private struct ScaledObjectRowView: View {
    let row: ScaledObjectRow

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 6) {
                Text(verbatim: row.name).font(.callout.monospaced()).lineLimit(1).truncationMode(.middle)
                    .foregroundStyle(.primary)
                Spacer()
                AutoscaleStateBadge(state: row.state)
            }
            Text(verbatim: [row.namespace, row.target.label].filter { !$0.isEmpty }.joined(separator: "  ·  "))
                .font(.caption).foregroundStyle(.secondary).lineLimit(1)
            if !row.triggers.isEmpty {
                Text(verbatim: row.triggers.map(\.label).joined(separator: "  ·  "))
                    .font(.caption).foregroundStyle(.secondary).lineLimit(1)
            }
        }
    }
}

private struct ScaledJobRowView: View {
    let row: ScaledJobRow

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 6) {
                Text(verbatim: row.name).font(.callout.monospaced()).lineLimit(1).truncationMode(.middle)
                    .foregroundStyle(.primary)
                Spacer()
                AutoscaleStateBadge(state: row.state)
            }
            Text(
                verbatim: [row.namespace, row.triggers.first?.label].compactMap { $0 }.filter { !$0.isEmpty }.joined(
                    separator: "  ·  ")
            )
            .font(.caption).foregroundStyle(.secondary).lineLimit(1)
        }
    }
}

/// Name, the target, and the first container's request → recommendation.
private struct VpaRowView: View {
    let row: VpaRow

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 6) {
                Text(verbatim: row.name).font(.callout.monospaced()).lineLimit(1).truncationMode(.middle)
                    .foregroundStyle(.primary)
                Spacer()
                AutoscaleStateBadge(state: row.state)
            }
            Text(
                verbatim: [row.namespace, row.target.label, row.updateMode].filter { !$0.isEmpty }.joined(
                    separator: "  ·  ")
            )
            .font(.caption).foregroundStyle(.secondary).lineLimit(1)
            if let first = row.containers.first {
                Text(verbatim: "\(first.name): \(vpaChangeText(first.cpuRequest, first.cpuTarget))")
                    .font(.caption).foregroundStyle(.secondary).lineLimit(1)
            }
        }
    }
}

/// Name, usage against its limits, and how many claims run in it.
private struct NodePoolRowView: View {
    let row: NodePoolRow
    let claimCount: Int

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 6) {
                Text(verbatim: row.name).font(.callout.monospaced()).lineLimit(1).truncationMode(.middle)
                    .foregroundStyle(.primary)
                Spacer()
                if row.atLimit {
                    AutoscaleChip(text: "at limit", color: .statusBad)
                } else {
                    AutoscaleStateBadge(state: row.state)
                }
            }
            Text(
                verbatim: [
                    AutoscaleText.poolUsage(row.resources.cpu, row.limits.cpu),
                    AutoscaleText.poolNodes(String(row.resources.nodes)),
                    AutoscaleText.poolClaims(String(claimCount)),
                ]
                .joined(separator: "  ·  ")
            )
            .font(.caption).foregroundStyle(.secondary).lineLimit(1)
        }
    }
}
