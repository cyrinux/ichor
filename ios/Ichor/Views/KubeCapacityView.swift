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
            // The rail only helps once the list is longer than a screen; without measuring the
            // content, a dozen node and workload rows is taken as that point.
            SectionRailScroll(sections: sections, showsRail: capacity.nodes.count + workloads.count >= Self.railMinimumRows) {
                List {
                    Section {
                        CapacityHeader(capacity: capacity) { loss = NodeLossRequest(node: "") }
                            .sectionTarget(Self.summaryId)
                    }
                    Section {
                        ForEach(capacity.nodes) { node in
                            CapacityNodeRow(node: node, metrics: capacity.metricsAvailable) { loss = NodeLossRequest(node: node.name) }
                        }
                    } header: {
                        Text(verbatim: CapacityText.capacityNodesTitle)
                            .sectionTarget(Self.nodesId)
                    }
                    Section {
                        if workloads.isEmpty {
                            Text(verbatim: CapacityText.capacityWorkloadsEmpty).foregroundStyle(.secondary)
                        }
                        ForEach(workloads) { CapacityWorkloadRow(workload: $0) }
                    } header: {
                        Text(verbatim: CapacityText.capacityWorkloadsTitle)
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
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Menu {
                    Picker(selection: $filter) {
                        ForEach(CapacityFilter.allCases, id: \.self) { Text(verbatim: $0.label).tag($0) }
                    } label: {
                        Text(verbatim: filter.label)
                    }
                } label: {
                    Image(systemName: filter == .all ? "line.3.horizontal.decrease.circle" : "line.3.horizontal.decrease.circle.fill")
                }
                .accessibilityLabel(Text(verbatim: filter.label))
            }
        }
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

/// The cluster's free room, the N+1 verdict, what could not be read, and "What if a node goes down".
private struct CapacityHeader: View {
    let capacity: KubeCapacity
    let onWhatIf: () -> Void

    var body: some View {
        let t = capacity.totals
        let n1 = capacity.nPlusOne
        VStack(alignment: .leading, spacing: 6) {
            StatusPill(label: n1.ok ? CapacityText.capacityNPlusOneOk : CapacityText.capacityNPlusOneShort(n1.node, "\(n1.pendingPods)"),
                       color: n1.ok ? .statusOK : .statusWarn)
            Text(verbatim: CapacityText.capacityFreeCpu(formatCPU(t.cpuFree), formatCPU(t.cpuAllocatable)))
            Text(verbatim: CapacityText.capacityFreeMemory(formatBytes(Int64(t.memoryFree)), formatBytes(Int64(t.memoryAllocatable))))
            Text(verbatim: CapacityText.capacityFreePods("\(t.podsFree)", "\(t.podCapacity)"))
            Group {
                if capacity.metricsForbidden {
                    Text(verbatim: CapacityText.capacityMetricsForbidden)
                } else if !capacity.metricsAvailable {
                    Text(verbatim: CapacityText.capacityMetricsUnavailable)
                }
                if !capacity.priorityClassesRead { Text(verbatim: CapacityText.capacityClassesUnread) }
                Text(verbatim: CapacityText.capacityIntro)
            }
            .font(.caption)
            .foregroundStyle(.secondary)
            if t.schedulableNodes > 1 {
                Button(action: onWhatIf) { Text(verbatim: CapacityText.capacityWhatIf) }
                    .buttonStyle(.bordered)
            }
        }
        .padding(.vertical, 4)
    }
}

/// One node: its memory requests and usage as two bars (usage only with metrics-server) and its
/// free room; a schedulable node opens what losing it costs.
private struct CapacityNodeRow: View {
    let node: CapacityNode
    let metrics: Bool
    let onOpen: () -> Void

    var body: some View {
        let requests = Int(node.memoryPercent.rounded())
        let usage = Int((node.memoryUsageFraction * 100).rounded())
        Button(action: onOpen) {
            VStack(alignment: .leading, spacing: 4) {
                HStack {
                    Text(verbatim: node.name).font(.callout.monospaced()).lineLimit(1).truncationMode(.middle)
                    Spacer()
                    ForEach(node.roles, id: \.self) { Tag($0) }
                    if node.overcommitted { Tag(CapacityText.capacityOvercommitted, color: Color.statusBad) }
                }
                VStack(alignment: .leading, spacing: 4) {
                    bar(CapacityText.capacityRequests, fraction: node.memoryPercent / 100, value: "\(requests)%")
                    if metrics { bar(CapacityText.capacityUsage, fraction: node.memoryUsageFraction, value: "\(usage)%") }
                }
                .accessibilityElement(children: .ignore)
                .accessibilityLabel(Text(verbatim: metrics
                    ? CapacityText.capacityNodeBars(node.name, "\(requests)", "\(usage)")
                    : CapacityText.capacityNodeBarsNoUsage(node.name, "\(requests)")))
                Text(verbatim: CapacityText.capacityNodeFree(formatCPU(node.cpuFree), formatBytes(Int64(node.memoryFree)), "\(node.podsFree)"))
                    .font(.caption).foregroundStyle(.secondary)
            }
            .padding(.vertical, 2)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .disabled(!node.schedulable)
        .accessibilityHint(Text(verbatim: CapacityText.capacityNodeOpen))
    }

    private func bar(_ label: String, fraction: Double, value: String) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            HStack {
                Text(verbatim: label).font(.caption).foregroundStyle(.secondary)
                Spacer()
                Text(verbatim: value).font(.caption.monospacedDigit()).foregroundStyle(.secondary)
            }
            UsageBar(fraction: fraction)
        }
    }
}

/// One workload: what one replica asks, then its priority class, priority, QoS, risk and how many more fit.
private struct CapacityWorkloadRow: View {
    let workload: CapacityWorkload

    var body: some View {
        let w = workload
        VStack(alignment: .leading, spacing: 4) {
            Text(verbatim: "\(w.kind) \(w.namespace)/\(w.name)").font(.callout.monospaced()).lineLimit(2).truncationMode(.middle)
            Text(verbatim: CapacityText.capacityWorkloadLine("\(w.replicas)", formatCPU(w.cpuRequest), formatBytes(Int64(w.memoryRequest))))
                .font(.caption).foregroundStyle(.secondary)
            ChipFlow {
                if w.noPriorityClass {
                    Tag(CapacityText.capacityNoClass, color: Color.statusWarn)
                } else {
                    Tag(w.priorityClassName, mono: true)
                }
                Tag(CapacityText.capacityPriority("\(w.priority)"))
                if w.qos != .unknown { Tag(w.qos.rawValue) }
                Tag(w.riskText, color: w.risk.color)
                if !w.moreFitNA {
                    Tag(CapacityText.capacityMoreFit("\(w.moreFit)"), color: w.moreFit > 0 ? Color.statusOK : Color.statusWarn)
                }
            }
        }
        .padding(.vertical, 2)
        .accessibilityElement(children: .combine)
    }
}

extension CapacityWorkload {
    /// Why it may be killed, and by whom.
    var riskText: String {
        switch risk {
        case .oom: oomKills > 0 ? CapacityText.capacityRiskOomKills("\(oomKills)") : CapacityText.capacityRiskOomNear
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
        case .evictFirst, .unbounded: .statusWarn
        case .ok: .statusOK
        }
    }
}
