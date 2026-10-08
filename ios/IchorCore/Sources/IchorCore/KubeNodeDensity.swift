import Foundation

/// The Kubernetes home's nodes, the way the overview handles a large Talos cluster
/// (NodeDensity): a status per node, counts, the problem nodes capped, and the search and
/// filter of the Kubernetes nodes screen. A kube node is never `.unreachable`: Kubernetes only
/// says ready or not. Same as Android's KubeNodeDensity.

public extension KubeNodeInfo {
    /// Not ready; else ready but cordoned or under pressure (`.attention`); else ready.
    var status: NodeStatus {
        if !ready { return .notReady }
        return cordoned || !pressure.isEmpty ? .attention : .ready
    }
}

/// The order the dense section and the nodes screen group by: the worst first.
public let kubeStatusOrder: [NodeStatus] = [.notReady, .attention, .ready]

/// The nodes of one status, in the core's order (control planes first, then by name).
public struct KubeStatusGroup: Equatable, Identifiable, Sendable {
    public let status: NodeStatus
    public let nodes: [KubeNodeInfo]

    public var id: NodeStatus { status }
}

/// The problem nodes the dense section shows in full (`shown`), and how many more there are.
public struct KubeProblemNodes: Equatable, Sendable {
    public let shown: [KubeNodeInfo]
    public let more: Int
}

/// The filters the Kubernetes nodes screen offers: no node is unreachable to Kubernetes.
public let kubeNodeFilters: [NodeFilter] = [.attention, .ready, .notReady]

public extension NodeFilter {
    func matches(_ node: KubeNodeInfo) -> Bool {
        switch self {
        case .attention: node.needsAttention
        case .ready: node.ready
        case .notReady: !node.ready
        case .unreachable: false
        }
    }
}

public extension Array where Element == KubeNodeInfo {
    var healthCounts: HealthCounts {
        HealthCounts(
            ready: filter { $0.status == .ready }.count,
            attention: filter { $0.status == .attention }.count,
            notReady: filter { $0.status == .notReady }.count
        )
    }

    /// Grouped by status, worst group first (`kubeStatusOrder`); empty groups left out.
    var byStatus: [KubeStatusGroup] {
        kubeStatusOrder.compactMap { status in
            let nodes = filter { $0.status == status }
            return nodes.isEmpty ? nil : KubeStatusGroup(status: status, nodes: nodes)
        }
    }

    /// The nodes needing attention, worst first (not ready, then cordoned or under pressure),
    /// in their given order otherwise; at most `max` of them, the rest counted.
    func problemNodes(max: Int = denseMaxProblems) -> KubeProblemNodes {
        let problems = byStatus.filter { $0.status != .ready }.flatMap(\.nodes)
        return KubeProblemNodes(shown: Array(problems.prefix(Swift.max(max, 0))), more: Swift.max(problems.count - max, 0))
    }

    /// The nodes whose name or address (internal or external) contains `query`, matching
    /// `filter` (nil: any).
    func filtered(query: String, filter: NodeFilter?) -> [KubeNodeInfo] {
        let q = query.trimmingCharacters(in: .whitespaces)
        return self.filter { node in
            (filter?.matches(node) ?? true) && (q.isEmpty || node.matches(query: q))
        }
    }
}

private extension KubeNodeInfo {
    func matches(query: String) -> Bool {
        name.localizedCaseInsensitiveContains(query)
            || (internalIP?.localizedCaseInsensitiveContains(query) ?? false)
            || (externalIP?.localizedCaseInsensitiveContains(query) ?? false)
    }
}
