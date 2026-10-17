import SwiftUI
import IchorCore

/// Capacity (os:admin): how much is free per node and for the cluster, whether one node's pods
/// would fit on the others, and each workload with what decides who goes first (priority, QoS
/// class, memory limit). A node, or "What if a node goes down", opens what losing it costs.
/// Reading it lists every pod: loaded on demand, never polled.
struct KubeCapacityView: View {
    @Environment(AppModel.self) private var model
    @State private var state: LoadState<KubeCapacity> = .loading
    @State private var filter = CapacityFilter.all
    /// The node whose loss the sheet shows ("" for the worst one).
    @State private var loss: NodeLossRequest?

    private static let summaryId = "summary"
    private static let nodesId = "nodes"
    private static let workloadsId = "workloads"
    private static let railMinimumRows = 12

    private var sections: [JumpSection] {
        [
            JumpSection(id: Self.summaryId, label: CapacityText.capacitySectionSummary),
            JumpSection(id: Self.nodesId, label: CapacityText.capacityNodesTitle),
            JumpSection(id: Self.workloadsId, label: CapacityText.capacitySectionWorkloads),
        ]
    }

    var body: some View {
        LoadStateView(state: state, retry: load) { capacity in
            let workloads = capacity.workloads.filtered(filter)
            // Hidden values come from the whole list, so a filter does not change what a row shows.
            let majority = CapacityMajority(capacity.workloads)
            // The rail only helps once the list is longer than a screen; without measuring the
            // content, a dozen node and workload rows is taken as that point.
            SectionRailScroll(sections: sections, showsRail: capacity.nodes.count + workloads.count >= Self.railMinimumRows) {
                List {
                    Section {
                        CapacityHeader(capacity: capacity) { loss = NodeLossRequest(node: "") }
                            .sectionTarget(Self.summaryId)
                    }
                    Section {
                        ForEach(capacity.nodes.overcommittedFirst) { node in
                            CapacityNodeRow(node: node, metrics: capacity.metricsAvailable) { loss = NodeLossRequest(node: node.name) }
                        }
                    } header: {
                        SectionHeader(title: CapacityText.capacityNodesTitle)
                            .sectionTarget(Self.nodesId)
                    }
                    Section {
                        Picker(selection: $filter) {
                            ForEach(CapacityFilter.allCases, id: \.self) { Text(verbatim: $0.label).tag($0) }
                        } label: {
                            Text(verbatim: CapacityText.capacitySectionWorkloads)
                        }
                        .pickerStyle(.segmented)
                        if workloads.isEmpty {
                            Text(verbatim: CapacityText.capacityWorkloadsEmpty).foregroundStyle(.secondary)
                        }
                        ForEach(workloads) { CapacityWorkloadRow(workload: $0, majority: majority) }
                    } header: {
                        SectionHeader(title: CapacityText.capacityWorkloadsTitle)
                            .sectionTarget(Self.workloadsId)
                    }
                }
                .refreshable { await load() }
                .themedBackground()
            }
        }
        .task { await load() }
        // A new API address (set on the Kubernetes screen): read again through it.
        .id(model.client?.kubeServer)
        .sheet(item: $loss) { KubeNodeLossSheet(start: $0.node) }
        .navigationTitle(Text(verbatim: CapacityText.capacityTitle))
        .navigationBarTitleDisplayMode(.inline)
    }

    private func load() async {
        guard let client = model.client else { return }
        let loaded: LoadState<KubeCapacity> = await .from { try await client.capacity() }
        state = state.refreshed(with: loaded)
    }
}

/// Which node's loss to show; "" for the worst one.
struct NodeLossRequest: Identifiable, Hashable {
    let node: String
    var id: String { node }
}

extension CapacityFilter {
    var label: String {
        switch self {
        case .all: CapacityText.capacityFilterAll
        case .atRisk: CapacityText.capacityFilterAtRisk
        case .noPriorityClass: CapacityText.capacityNoClass
        }
    }
}

/// A trailing pill wraps rather than squeeze the row's title away.
private let pillMaxWidth: CGFloat = 160

/// A node's memory meters turn amber from 80 %, later than the usual 75 %.
private let nodeWarnAt = 0.8

/// The N+1 verdict, the cluster's free room as tiles (CPU, memory, pod slots, and ephemeral
/// storage when the nodes report it: then two rows of two), what could not be read, and
/// "What if a node goes down".
private struct CapacityHeader: View {
    let capacity: KubeCapacity
    let onWhatIf: () -> Void

    var body: some View {
        let t = capacity.totals
        let n1 = capacity.nPlusOne
        VStack(alignment: .leading, spacing: 10) {
            HStack(alignment: .center) {
                StatusPill(label: nPlusOneLabel(n1), color: n1.ok ? .statusOK : .statusWarn)
                Spacer(minLength: 4)
                InfoHint(title: Text(verbatim: CapacityText.capacityTitle), text: Text(verbatim: CapacityText.capacityIntro))
            }
            tiles(t)
            notes
            if t.schedulableNodes > 1 {
                Button(action: onWhatIf) { Text(verbatim: CapacityText.capacityWhatIf) }
                    .buttonStyle(.bordered)
            }
        }
        .padding(.vertical, 4)
    }

    /// Three tiles in a row; four (ephemeral storage reported) in two rows of two.
    @ViewBuilder
    private func tiles(_ t: CapacityTotals) -> some View {
        let cpu = FreeTile(free: formatCPU(t.cpuFree), of: formatCPU(t.cpuAllocatable), label: CapacityText.capacityTileCpuFree,
                           used: freeFraction(t.cpuRequests, of: t.cpuAllocatable))
        let memory = FreeTile(free: formatBytes(Int64(t.memoryFree)), of: formatBytes(Int64(t.memoryAllocatable)),
                              label: CapacityText.capacityTileMemoryFree,
                              used: freeFraction(t.memoryRequests, of: t.memoryAllocatable))
        let pods = FreeTile(free: "\(t.podsFree)", of: "\(t.podCapacity)", label: CapacityText.capacityTilePods,
                            used: freeFraction(Double(t.pods), of: Double(t.podCapacity)))
        // No allocatable ephemeral storage reported (old kubelets, some managed clusters): unknown, not full.
        if t.ephemeralAllocatable > 0 {
            let ephemeral = FreeTile(free: formatBytes(Int64(t.ephemeralFree)), of: formatBytes(Int64(t.ephemeralAllocatable)),
                                     label: CapacityText.capacityTileEphemeralFree,
                                     used: freeFraction(t.ephemeralRequests, of: t.ephemeralAllocatable))
            Grid(alignment: .topLeading, horizontalSpacing: 12, verticalSpacing: 10) {
                GridRow { cpu; memory }
                GridRow { pods; ephemeral }
            }
        } else {
            HStack(alignment: .top, spacing: 12) { cpu; memory; pods }
        }
    }

    /// The disk, metrics and priority-class notes, one muted line each.
    private var notes: some View {
        VStack(alignment: .leading, spacing: 4) {
            if capacity.diskMeasured, let fullest = capacity.fullestDiskNode {
                Text(verbatim: CapacityText.capacityDiskFullest(formatBytes(Int64(fullest.diskAvailable)), fullest.name))
            } else {
                Text(verbatim: capacity.diskForbidden ? CapacityText.capacityDiskForbidden : CapacityText.capacityDiskUnmeasured)
            }
            if capacity.metricsForbidden {
                Text(verbatim: CapacityText.capacityMetricsForbidden)
            } else if !capacity.metricsAvailable {
                Text(verbatim: CapacityText.capacityMetricsUnavailable)
            }
            if !capacity.priorityClassesRead { Text(verbatim: CapacityText.capacityClassesUnread) }
        }
        .font(.caption)
        .foregroundStyle(.secondary)
    }

    private func nPlusOneLabel(_ n1: NPlusOne) -> String {
        if n1.ok { return CapacityText.capacityNPlusOneOk }
        return n1.shortEphemeral > 0
            ? CapacityText.capacityNPlusOneShortDisk(n1.node, "\(n1.pendingPods)")
            : CapacityText.capacityNPlusOneShort(n1.node, "\(n1.pendingPods)")
    }
}

/// "31.2 of 60.6" over "CPU free", with the requested share as a bar.
private struct FreeTile: View {
    let free: String
    let of: String
    let label: String
    let used: Double

    var body: some View {
        StatTile(icon: nil, value: CapacityText.capacityTileOf(free, of), label: label, meter: { AnyView(UsageBar(fraction: used)) })
            .frame(maxWidth: .infinity, alignment: .leading)
    }
}

/// One node: its name, its roles as text, its memory requests and usage as two meters (usage
/// only with metrics-server), its free room and disk; a schedulable node opens what losing it
/// costs. One pill at most: overcommitted, else DiskPressure, else disk low.
private struct CapacityNodeRow: View {
    let node: CapacityNode
    let metrics: Bool
    let onOpen: () -> Void

    var body: some View {
        let requests = Int(node.memoryPercent.rounded())
        let usage = Int((node.memoryUsageFraction * 100).rounded())
        Button(action: onOpen) {
            ResourceRow(health: nil, title: node.name, subtitle: node.roleLine, trailing: pill) {
                VStack(alignment: .leading, spacing: 4) {
                    VStack(alignment: .leading, spacing: 4) {
                        MeterRow(label: CapacityText.capacityRequests, fraction: node.memoryPercent / 100,
                                 value: "\(requests)%", warnAt: nodeWarnAt)
                        if metrics {
                            MeterRow(label: CapacityText.capacityUsage, fraction: node.memoryUsageFraction,
                                     value: "\(usage)%", warnAt: nodeWarnAt)
                        }
                    }
                    .accessibilityElement(children: .ignore)
                    .accessibilityLabel(Text(verbatim: metrics
                        ? CapacityText.capacityNodeBars(node.name, "\(requests)", "\(usage)")
                        : CapacityText.capacityNodeBarsNoUsage(node.name, "\(requests)")))
                    Text(verbatim: CapacityText.capacityNodeFree(formatCPU(node.cpuFree), formatBytes(Int64(node.memoryFree)), "\(node.podsFree)"))
                        .font(.caption).foregroundStyle(.secondary)
                    if node.diskKnown {
                        Text(verbatim: CapacityText.capacityNodeDisk(formatBytes(Int64(node.diskAvailable)), formatBytes(Int64(node.diskCapacity))))
                            .font(.caption).foregroundStyle(.secondary)
                    }
                }
                .padding(.top, 4)
            }
            .padding(.vertical, 2)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .disabled(!node.schedulable)
        .accessibilityHint(Text(verbatim: CapacityText.capacityNodeOpen))
    }

    @ViewBuilder
    private var pill: some View {
        switch node.flag {
        case .overcommitted: CapacityPill(label: CapacityText.capacityOvercommitted, color: .statusBad)
        case .diskPressure: CapacityPill(label: CapacityText.capacityNodeDiskPressure, color: .statusBad)
        case .diskLow: CapacityPill(label: CapacityText.capacityNodeDiskLow, color: .statusWarn)
        case nil: EmptyView()
        }
    }
}

/// A row's one StatusPill, wrapping within `pillMaxWidth` rather than squeezing the title.
private struct CapacityPill: View {
    let label: String
    let color: Color

    var body: some View {
        // Spelled out: a trailing closure right after the initializer would go to the init.
        CappedWidth(maxWidth: pillMaxWidth).callAsFunction {
            StatusPill(label: label, color: color)
        }
    }
}

/// Its one subview at its own width, never wider than `maxWidth` (a longer text wraps), set
/// against the trailing edge.
private struct CappedWidth: Layout {
    let maxWidth: CGFloat

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        guard let child = subviews.first else { return .zero }
        let width = min(proposal.width ?? maxWidth, maxWidth)
        return child.sizeThatFits(ProposedViewSize(width: width, height: nil))
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        subviews.first?.place(at: CGPoint(x: bounds.maxX, y: bounds.midY), anchor: .trailing,
                              proposal: ProposedViewSize(width: bounds.width, height: nil))
    }
}

/// One workload: what its replicas ask and how many more fit, its risk as the only pill (none
/// when it is fine), then its kind and the priority class, priority and QoS that differ from
/// the list's majority.
private struct CapacityWorkloadRow: View {
    let workload: CapacityWorkload
    let majority: CapacityMajority

    var body: some View {
        let w = workload
        ResourceRow(health: health, title: "\(w.namespace)/\(w.name)", subtitle: subtitle, trailing: pill) {
            HStack(spacing: 6) {
                Tag(kindShort)
                if !traits.isEmpty {
                    Text(verbatim: traits).font(.caption).foregroundStyle(.secondary).lineLimit(1).truncationMode(.tail)
                }
            }
            .padding(.top, 2)
        }
        .padding(.vertical, 2)
        .accessibilityElement(children: .combine)
    }

    @ViewBuilder
    private var pill: some View {
        if workload.atRisk { CapacityPill(label: workload.riskText, color: workload.risk.color) }
    }

    /// "3 × 500m CPU · 1.0 GiB · 35 more fit"; its ephemeral storage only when it asks for some.
    private var subtitle: String {
        let w = workload
        let parts = [
            CapacityText.capacityWorkloadReplicas("\(w.replicas)", formatCPU(w.cpuRequest)),
            formatBytes(Int64(w.memoryRequest)),
            w.ephemeralRequest > 0 ? CapacityText.capacityWorkloadEphemeral(formatBytes(Int64(w.ephemeralRequest))) : nil,
            w.moreFitNA ? nil : CapacityText.capacityMoreFit("\(w.moreFit)"),
            w.atRisk ? nil : CapacityText.capacityRiskOk,
        ]
        return parts.compactMap { $0 }.joined(separator: " · ")
    }

    /// "platform-optional · priority 10 · Burstable" without the majority values.
    private var traits: String {
        workload.details(against: majority).map { detail -> String in
            switch detail {
            case .noPriorityClass: CapacityText.capacityNoClass
            case let .priorityClass(name): name
            case let .priority(value): CapacityText.capacityPriority("\(value)")
            case let .qos(qos): qos.rawValue
            }
        }
        .joined(separator: " · ")
    }

    /// OOM is bad, any other risk or a missing priority class a warning, nothing otherwise.
    private var health: ServiceHealth? {
        if workload.risk == .oom { return .critical }
        if workload.atRisk || workload.noPriorityClass { return .warning }
        return nil
    }

    /// Deployment, StatefulSet and DaemonSet as kubectl shortens them; any other kind as it is.
    private var kindShort: String {
        switch workload.kind {
        case "Deployment": CapacityText.capacityKindDeploymentShort
        case "StatefulSet": CapacityText.capacityKindStatefulsetShort
        case "DaemonSet": CapacityText.capacityKindDaemonsetShort
        default: workload.kind
        }
    }
}

extension CapacityWorkload {
    /// Why it may be killed, and by whom.
    var riskText: String {
        switch risk {
        case .oom: oomKills > 0 ? CapacityText.capacityRiskOomKills("\(oomKills)") : CapacityText.capacityRiskOomNear
        case .diskNear: CapacityText.capacityRiskDiskNear
        case .evictFirst: CapacityText.capacityRiskEvictFirst
        case .unbounded: CapacityText.capacityRiskUnbounded
        case .ok: CapacityText.capacityRiskOk
        }
    }
}

extension CapacityRisk {
    var color: Color {
        switch self {
        case .oom: .statusBad
        case .diskNear, .evictFirst, .unbounded: .statusWarn
        case .ok: .statusOK
        }
    }
}
