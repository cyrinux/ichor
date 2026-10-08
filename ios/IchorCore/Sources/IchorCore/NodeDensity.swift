import Foundation

/// Above this many nodes, the overview's nodes section turns dense: a dot per node instead of a
/// chip with its hostname, problems capped, and "expand" opens the Nodes screen (same as Android).
public let nodeDenseThreshold = 24

/// Problem nodes the dense section lists in full; the others are behind "N more".
public let denseMaxProblems = 5

/// Seconds between live cluster samples: live enough to read, light on the nodes and the phone's data.
public let liveStatsPollSeconds = 5

/// The same on a dense cluster: every sample asks every node, so fewer of them.
public let denseLiveStatsPollSeconds = 15

/// Whether a cluster of `nodeCount` nodes gets the dense overview.
public func isDenseCluster(_ nodeCount: Int) -> Bool { nodeCount > nodeDenseThreshold }

/// Seconds between live cluster samples for a cluster of `nodeCount` nodes (Android's clusterPollSeconds).
public func clusterPollSeconds(_ nodeCount: Int) -> Int {
    isDenseCluster(nodeCount) ? denseLiveStatsPollSeconds : liveStatsPollSeconds
}

/// A node's dot in the dense section: its `NodeHealth`, except that a ready node reporting a
/// problem (`needsAttention`) is `attention`, so a node listed as a problem never shows as calm.
public enum NodeStatus: String, CaseIterable, Sendable {
    case ready, attention, notReady, unreachable
}

/// What the dense home sections and the nodes screens need of a node, Talos (`NodeOverview`)
/// or Kubernetes (`KubeNodeInfo`): the counts, the problem list, the grouping and the filters
/// below serve both. Same as Android's NodeDensity.
public protocol NodeStatusProviding {
    /// The dot's status: ready, ready but needing attention, not ready, unreachable.
    var status: NodeStatus { get }
    /// Ready as the cluster reports it; a ready node may still need attention.
    var isReady: Bool { get }
    /// Its name or an address contains `query`, case-insensitively.
    func matches(query: String) -> Bool
}

public extension NodeOverview {
    var status: NodeStatus {
        switch health {
        case .unreachable: .unreachable
        case .notReady: .notReady
        case .ready: needsAttention ? .attention : .ready
        }
    }
}

extension NodeOverview: NodeStatusProviding {
    public var isReady: Bool { health == .ready }

    /// The hostname, or an address (talosconfig or public).
    public func matches(query: String) -> Bool {
        hostname.localizedCaseInsensitiveContains(query)
            || node.localizedCaseInsensitiveContains(query)
            || publicIPs.contains { $0.localizedCaseInsensitiveContains(query) }
    }
}

/// How many nodes are in each `NodeStatus`.
public struct HealthCounts: Equatable, Sendable {
    public var ready = 0
    public var attention = 0
    public var notReady = 0
    public var unreachable = 0

    public init(ready: Int = 0, attention: Int = 0, notReady: Int = 0, unreachable: Int = 0) {
        self.ready = ready
        self.attention = attention
        self.notReady = notReady
        self.unreachable = unreachable
    }

    public var total: Int { ready + attention + notReady + unreachable }

    public subscript(status: NodeStatus) -> Int {
        switch status {
        case .ready: ready
        case .attention: attention
        case .notReady: notReady
        case .unreachable: unreachable
        }
    }
}

/// The order the dense sections and the nodes screens group by: the worst first.
public let nodeStatusOrder: [NodeStatus] = [.unreachable, .notReady, .attention, .ready]

/// The nodes of one status, in their given order (the core's: control planes first, then by name).
public struct StatusGroup<Node: Equatable & Sendable>: Equatable, Identifiable, Sendable {
    public let status: NodeStatus
    public let nodes: [Node]

    public var id: NodeStatus { status }
}

/// The problem nodes the dense section shows in full (`shown`), and how many more there are.
public struct ProblemNodes<Node: Equatable & Sendable>: Equatable, Sendable {
    public let shown: [Node]
    public let more: Int
}

/// What the Nodes screen narrows its list to, besides the search.
public enum NodeFilter: String, CaseIterable, Hashable, Sendable {
    /// Down, not ready, or reporting a problem: the dense section's "N more".
    case attention, ready, notReady, unreachable

    public func matches<Node: NodeStatusProviding>(_ node: Node) -> Bool {
        switch self {
        case .attention: node.status != .ready
        case .ready: node.isReady
        case .notReady: node.status == .notReady
        case .unreachable: node.status == .unreachable
        }
    }
}

public extension Array where Element: NodeStatusProviding & Equatable & Sendable {
    var healthCounts: HealthCounts {
        HealthCounts(
            ready: filter { $0.status == .ready }.count,
            attention: filter { $0.status == .attention }.count,
            notReady: filter { $0.status == .notReady }.count,
            unreachable: filter { $0.status == .unreachable }.count
        )
    }

    /// Grouped by status, the worst group first (`nodeStatusOrder`), each in the given order;
    /// empty groups left out.
    var byStatus: [StatusGroup<Element>] {
        nodeStatusOrder.compactMap { status in
            let nodes = filter { $0.status == status }
            return nodes.isEmpty ? nil : StatusGroup(status: status, nodes: nodes)
        }
    }

    /// The nodes needing attention, worst first (unreachable, not ready, then ready but reporting
    /// a problem), in their given order otherwise; at most `max` of them, the rest counted.
    func problemNodes(max: Int = denseMaxProblems) -> ProblemNodes<Element> {
        let problems = byStatus.filter { $0.status != .ready }.flatMap(\.nodes)
        return ProblemNodes(shown: Array(problems.prefix(Swift.max(max, 0))), more: Swift.max(problems.count - max, 0))
    }

    /// The nodes whose name or an address contains `query`, matching `filter` (nil: any).
    func filtered(query: String, filter: NodeFilter?) -> [Element] {
        let q = query.trimmingCharacters(in: .whitespaces)
        return self.filter { node in
            (filter?.matches(node) ?? true) && (q.isEmpty || node.matches(query: q))
        }
    }
}

public extension Array where Element == NodeOverview {
    /// As the overview lists them: control-plane nodes first, then by hostname.
    var overviewOrder: [NodeOverview] {
        sorted { (overviewRank($0), $0.hostname) < (overviewRank($1), $1.hostname) }
    }
}

public extension Array where Element == NodeGroup {
    /// The groups narrowed to the nodes whose hostname or address (talosconfig or public)
    /// contains `query`, matching `filter` (nil: any), on the site keyed `site` (nil: any);
    /// groups left empty are dropped. Same as Android's filterNodes.
    func filtered(query: String, filter: NodeFilter?, site: String?) -> [NodeGroup] {
        self.filter { site == nil || $0.key == site }
            .map { NodeGroup(site: $0.site, nodes: $0.nodes.filtered(query: query, filter: filter)) }
            .filter { !$0.nodes.isEmpty }
    }
}

private func overviewRank(_ node: NodeOverview) -> Int {
    node.role == "controlplane" ? 0 : 1
}
