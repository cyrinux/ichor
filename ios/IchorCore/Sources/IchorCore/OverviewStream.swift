import Foundation

// The overview streamed node by node (Go StartClusterOverview): each node as it answers, then
// the complete overview. Same rules as Android's OverviewViewModel.fetch and showPartial.

/// What a streamed overview reports: one node as soon as it answered or failed (in completion
/// order, without the public IPs only its peers see), then the complete overview.
public enum OverviewPart: Equatable, Sendable {
    case node(NodeOverview)
    case done(ClusterOverview)
}

/// The nodes an overview asks, as the Go core picks them (targetNodes): the context's nodes,
/// else its endpoints (never for Omni, whose nodes come from Omni), each once, in order.
public func overviewTargets(nodes: [String], endpoints: [String], omni: Bool) -> [String] {
    let candidates = nodes.isEmpty && !omni ? endpoints : nodes
    var seen = Set<String>()
    return candidates.filter { seen.insert($0).inserted }
}

/// The nodes a streamed overview has so far: in `targets`' order (the order of the complete
/// overview, so rows do not move when it lands), those it does not list after, as they came.
public struct OverviewAssembly: Equatable, Sendable {
    public let context: String
    public let targets: [String]
    public let nodes: [NodeOverview]

    public init(context: String, targets: [String], nodes: [NodeOverview] = []) {
        self.context = context
        self.targets = targets
        self.nodes = nodes
    }

    /// This assembly with `node`; a node already there (same address) is replaced.
    public func adding(_ node: NodeOverview) -> OverviewAssembly {
        let others = nodes.filter { $0.node != node.node }
        return OverviewAssembly(context: context, targets: targets, nodes: others + [node])
    }

    /// The overview to show while the rest loads: marked partial, so nothing stores it.
    public var overview: ClusterOverview {
        let rank = Dictionary(targets.enumerated().map { ($1, $0) }, uniquingKeysWith: { first, _ in first })
        let ordered = nodes.enumerated().sorted { a, b in
            let ra = rank[a.element.node] ?? targets.count + a.offset
            let rb = rank[b.element.node] ?? targets.count + b.offset
            return ra < rb
        }
        return ClusterOverview(context: context, nodes: ordered.map(\.element), partial: true)
    }
}

/// How far a partial overview got: "7 of 8 nodes answered", or "Nodes answered: 7" when `total` is
/// unknown (nil).
public struct NodesAnswered: Equatable, Sendable {
    public let answered: Int
    public let total: Int?

    /// `total` 0 or less is unknown; never fewer than `answered` (a node the targets missed).
    public init(answered: Int, total: Int?) {
        self.answered = answered
        self.total = total.flatMap { $0 > 0 ? max($0, answered) : nil }
    }

    /// For the thin progress bar; nil when the total is unknown.
    public var fraction: Double? {
        total.map { Double(answered) / Double($0) }
    }
}

extension ClusterOverview {
    /// The progress line of a partial overview asked of `targets` (nil when complete).
    public func answered(of targets: [String]) -> NodesAnswered? {
        partial ? NodesAnswered(answered: nodes.count, total: targets.count) : nil
    }
}

extension LoadState where T == ClusterOverview {
    /// A partial overview on screen (one `showingPartial` put there).
    public var showsPartial: Bool {
        if case .loaded(let overview, _, _) = self { return overview.partial }
        return false
    }

    /// The state with `partial` on screen, nil when it must not replace what is shown: only a
    /// screen still loading, or one showing an earlier part, takes it. The previous load, the
    /// last known overview and the background check's seed stay until the complete overview
    /// (L12: a refresh or a cluster switch keeps the old rows).
    public func showingPartial(_ partial: ClusterOverview, at date: Date = Date()) -> LoadState<ClusterOverview>? {
        switch self {
        case .loading: return .loaded(partial, at: date)
        case .loaded where showsPartial: return .loaded(partial, at: date)
        default: return nil
        }
    }

    /// The state once the stream ended with `final`: `refreshed(with:)`, except that a failed
    /// stream does not keep its own partial nodes as if they were data (the error page shows).
    public func finished(with final: LoadState<ClusterOverview>) -> LoadState<ClusterOverview> {
        showsPartial ? final : refreshed(with: final)
    }

    /// What the overview on screen counts as earlier data: nothing while it is a partial one.
    public var completeOverview: (overview: ClusterOverview, at: Date)? {
        if case .loaded(let overview, let at, _) = self, !overview.partial { return (overview, at) }
        return nil
    }
}
