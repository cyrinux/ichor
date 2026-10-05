import Foundation

/// Above this many nodes, the overview's nodes section turns dense: a dot per node instead of a
/// chip with its hostname, problems capped, and "expand" opens the Nodes screen (same as Android).
public let nodeDenseThreshold = 24

/// Problem nodes the dense section lists in full; the others are behind "N more".
public let denseMaxProblems = 5

/// Whether a cluster of `nodeCount` nodes gets the dense overview.
public func isDenseCluster(_ nodeCount: Int) -> Bool { nodeCount > nodeDenseThreshold }

/// How many nodes are in each `NodeHealth`.
public struct HealthCounts: Equatable, Sendable {
    public var ready = 0
    public var notReady = 0
    public var unreachable = 0

    public init(ready: Int = 0, notReady: Int = 0, unreachable: Int = 0) {
        self.ready = ready
        self.notReady = notReady
        self.unreachable = unreachable
    }

    public var total: Int { ready + notReady + unreachable }

    public subscript(health: NodeHealth) -> Int {
        switch health {
        case .ready: ready
        case .notReady: notReady
        case .unreachable: unreachable
        }
    }
}

/// The problem nodes the dense section shows in full (`shown`), and how many more there are.
public struct ProblemNodes: Equatable, Sendable {
    public let shown: [NodeOverview]
    public let more: Int
}

/// What the Nodes screen narrows its list to, besides the search.
public enum NodeFilter: String, CaseIterable, Hashable, Sendable {
    /// Down, not ready, or reporting a problem: the dense section's "N more".
    case attention, ready, notReady, unreachable

    public func matches(_ node: NodeOverview) -> Bool {
        switch self {
        case .attention: node.needsAttention
        case .ready: node.health == .ready
        case .notReady: node.health == .notReady
        case .unreachable: node.health == .unreachable
        }
    }
}

public extension Array where Element == NodeOverview {
    /// As the overview lists them: control-plane nodes first, then by hostname.
    var overviewOrder: [NodeOverview] {
        sorted { (overviewRank($0), $0.hostname) < (overviewRank($1), $1.hostname) }
    }

    var healthCounts: HealthCounts {
        HealthCounts(
            ready: filter { $0.health == .ready }.count,
            notReady: filter { $0.health == .notReady }.count,
            unreachable: filter { $0.health == .unreachable }.count
        )
    }

    /// The nodes needing attention, worst first (unreachable, not ready, then ready but reporting
    /// a problem), in their given order otherwise; at most `max` of them, the rest counted.
    func problemNodes(max: Int = denseMaxProblems) -> ProblemNodes {
        let problems = enumerated()
            .filter { $0.element.needsAttention }
            .sorted { (severity($0.element.health), $0.offset) < (severity($1.element.health), $1.offset) }
            .map(\.element)
        return ProblemNodes(shown: Array(problems.prefix(Swift.max(max, 0))), more: Swift.max(problems.count - max, 0))
    }

    /// The nodes whose hostname or address (talosconfig or public) contains `query`, matching
    /// `filter` (nil: any).
    func filtered(query: String, filter: NodeFilter?) -> [NodeOverview] {
        let q = query.trimmingCharacters(in: .whitespaces)
        return self.filter { node in
            (filter?.matches(node) ?? true) && (q.isEmpty || node.matches(query: q))
        }
    }
}

private func overviewRank(_ node: NodeOverview) -> Int {
    node.role == "controlplane" ? 0 : 1
}

private func severity(_ health: NodeHealth) -> Int {
    switch health {
    case .unreachable: 0
    case .notReady: 1
    case .ready: 2
    }
}

private extension NodeOverview {
    func matches(query: String) -> Bool {
        hostname.localizedCaseInsensitiveContains(query)
            || node.localizedCaseInsensitiveContains(query)
            || publicIPs.contains { $0.localizedCaseInsensitiveContains(query) }
    }
}
