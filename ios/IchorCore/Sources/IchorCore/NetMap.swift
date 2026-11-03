import Foundation

// Mirrors go/kube/netmap: how traffic reaches pods, as a layered graph read left to right (the
// caller's entry boxes, such as an Argo CD app's host → Gateway → route, then Service → pod →
// node), and the pure logic a map view draws it with: its columns, the path through a tapped
// box, the boxes folded behind "+N more".

public enum NetMapKind: String, Sendable, CaseIterable, WireEnum {
    case host = "Host"
    case loadBalancer = "LoadBalancer"
    case gateway = "Gateway"
    case ingress = "Ingress"
    case httpRoute = "HTTPRoute"
    case grpcRoute = "GRPCRoute"
    case tlsRoute = "TLSRoute"
    case ingressRoute = "IngressRoute"
    case ingressRouteTCP = "IngressRouteTCP"
    case service = "Service"
    case pod = "Pod"
    case node = "Node"
    case unknown = ""

    public static let wireFallback: Self = .unknown

    /// A Gateway or a route, which a shared one (not the caller's) can be.
    public var routesTraffic: Bool {
        switch self {
        case .gateway, .ingress, .httpRoute, .grpcRoute, .tlsRoute, .ingressRoute, .ingressRouteTCP: true
        case .host, .loadBalancer, .service, .pod, .node, .unknown: false
        }
    }
}

/// A network map; empty when nothing sends traffic anywhere.
public struct NetMap: Decodable, Equatable, Sendable {
    /// By layer, worst first.
    public let nodes: [NetMapNode]
    public let edges: [NetMapEdge]
    /// The deepest broken box, the likely root cause; nil when every box is fine.
    public let problem: NetMapProblem?

    public init(nodes: [NetMapNode] = [], edges: [NetMapEdge] = [], problem: NetMapProblem? = nil) {
        self.nodes = nodes
        self.edges = edges
        self.problem = problem
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        nodes = try c.field(.nodes, [])
        edges = try c.field(.edges, [])
        problem = try c.decodeIfPresent(NetMapProblem.self, forKey: .problem)
    }

    private enum CodingKeys: String, CodingKey { case nodes, edges, problem }
}

/// One box: a Service, a pod, a node, or one of the caller's (a host, a Gateway, a route…).
public struct NetMapNode: Decodable, Equatable, Identifiable, Sendable {
    public let id: String
    /// The column, left to right: the caller's own layers first, then Service, pod and node.
    public let layer: Int
    public let kind: NetMapKind
    /// The kind as sent, for one this app does not know yet.
    public let kindName: String
    public let namespace: String
    public let name: String
    /// "ClusterIP 10.96.0.12 · 80→8080", a pod's status, an address.
    public let detail: String
    public let health: ServiceHealth
    /// What a host opens, "" otherwise.
    public let url: String
    /// The caller's own resource: false for a shared Gateway or route, pods and nodes.
    public let managed: Bool

    public init(
        id: String, layer: Int, kind: NetMapKind, namespace: String = "", name: String,
        detail: String = "", health: ServiceHealth = .ok, url: String = "", managed: Bool = false
    ) {
        self.id = id
        self.layer = layer
        self.kind = kind
        self.kindName = kind.rawValue
        self.namespace = namespace
        self.name = name
        self.detail = detail
        self.health = health
        self.url = url
        self.managed = managed
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.field(.id, "")
        layer = max(try c.field(.layer, 0), 0)
        kind = try c.wire(.kind)
        kindName = try c.field(.kind, "")
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        detail = try c.field(.detail, "")
        health = try c.wire(.health)
        url = try c.field(.url, "")
        managed = try c.field(.managed, false)
    }

    /// Shared with other apps: drawn with a dashed outline.
    public var shared: Bool { kind.routesTraffic && !managed }

    /// The same box with another health (a node Talos reports down).
    func with(health: ServiceHealth, detail: String) -> NetMapNode {
        NetMapNode(
            id: id, layer: layer, kind: kind, namespace: namespace, name: name, detail: detail,
            health: health, url: url, managed: managed)
    }

    private enum CodingKeys: String, CodingKey { case id, layer, kind, namespace, name, detail, health, url, managed }
}

/// One hop, coloured by its target: traffic stops where it is red.
public struct NetMapEdge: Decodable, Equatable, Hashable, Identifiable, Sendable {
    public let from: String
    public let to: String
    public let health: ServiceHealth

    public var id: String { "\(from)>\(to)" }

    public init(from: String, to: String, health: ServiceHealth = .ok) {
        self.from = from
        self.to = to
        self.health = health
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        from = try c.field(.from, "")
        to = try c.field(.to, "")
        health = try c.wire(.health)
    }

    private enum CodingKeys: String, CodingKey { case from, to, health }
}

/// The likely root cause, for the caller to word.
public struct NetMapProblem: Decodable, Equatable, Sendable {
    public let kind: NetMapKind
    public let namespace: String
    public let name: String
    public let detail: String

    public init(kind: NetMapKind, namespace: String = "", name: String, detail: String = "") {
        self.kind = kind
        self.namespace = namespace
        self.name = name
        self.detail = detail
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        kind = try c.wire(.kind)
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        detail = try c.field(.detail, "")
    }

    private enum CodingKeys: String, CodingKey { case kind, namespace, name, detail }
}

/// One column of the graph; hidden: the boxes folded behind "+N more".
public struct NetMapColumn: Equatable, Identifiable, Sendable {
    public let layer: Int
    public let nodes: [NetMapNode]
    public let hidden: [NetMapNode]
    /// More boxes than a folded column shows: it has a "+N more" (or "Show less") box.
    public let folds: Bool

    public var id: Int { layer }

    /// The kind its title is taken from, that of its first box.
    public var kind: NetMapKind { (nodes.first ?? hidden.first)?.kind ?? .unknown }
    /// The kind as sent, for a title this app has no word for.
    public var kindName: String { (nodes.first ?? hidden.first)?.kindName ?? "" }
}

/// What a tapped box lights up: everything upstream and downstream of it.
public struct NetMapPath: Equatable, Sendable {
    public let nodes: Set<String>
    public let edges: Set<String>

    public func contains(node id: String) -> Bool { nodes.contains(id) }
    public func contains(edge id: String) -> Bool { edges.contains(id) }
}

public extension NetMap {
    /// Boxes a column shows before the rest folds behind "+N more".
    static let columnLimit = 8

    /// The id of the "+N more" box the folded boxes of layer have their hops drawn to.
    static func moreID(layer: Int) -> String { "more/\(layer)" }

    var isEmpty: Bool { nodes.isEmpty }

    /// The non-empty layers, left to right, each worst first (as sent). Beyond limit boxes, the
    /// rest of a column folds unless expanded.
    func columns(expanded: Bool = false, limit: Int = NetMap.columnLimit) -> [NetMapColumn] {
        let byLayer = Dictionary(grouping: nodes, by: \.layer)
        return byLayer.keys.sorted().compactMap { layer in
            guard let members = byLayer[layer], !members.isEmpty else { return nil }
            let folds = members.count > limit
            guard folds, !expanded else {
                return NetMapColumn(layer: layer, nodes: members, hidden: [], folds: folds)
            }
            return NetMapColumn(
                layer: layer, nodes: Array(members.prefix(limit)), hidden: Array(members.dropFirst(limit)),
                folds: true)
        }
    }

    /// The hops to draw with the boxes of hidden folded: theirs go to their column's "+N more"
    /// box instead, once per pair, with the worst health of those it stands for.
    func edges(hiding hidden: Set<String>) -> [NetMapEdge] {
        guard !hidden.isEmpty else { return edges }
        let shownID = proxy(hiding: hidden)
        var order: [String] = []
        var merged: [String: NetMapEdge] = [:]
        for edge in edges {
            let shown = NetMapEdge(from: shownID(edge.from), to: shownID(edge.to), health: edge.health)
            if let known = merged[shown.id] {
                merged[shown.id] = NetMapEdge(from: shown.from, to: shown.to, health: min(known.health, shown.health))
            } else {
                order.append(shown.id)
                merged[shown.id] = shown
            }
        }
        return order.compactMap { merged[$0] }
    }

    /// Every box and hop traffic through id takes: upstream to the entry points, downstream to
    /// the nodes. With boxes hidden, their "+N more" box and its hops light up for them.
    func path(through id: String, hiding hidden: Set<String> = []) -> NetMapPath {
        let upstream = walk(from: id, next: { edge in edge.to }, step: { edge in edge.from })
        let downstream = walk(from: id, next: { edge in edge.from }, step: { edge in edge.to })
        var nodes = upstream.nodes.union(downstream.nodes)
        let lit = upstream.edges + downstream.edges
        var edgeIDs = Set(lit.map(\.id))
        if !hidden.isEmpty {
            let shownID = proxy(hiding: hidden)
            nodes.formUnion(nodes.intersection(hidden).map(shownID))
            edgeIDs.formUnion(lit.map { NetMapEdge(from: shownID($0.from), to: shownID($0.to)).id })
        }
        return NetMapPath(nodes: nodes, edges: edgeIDs)
    }

    /// The graph with the nodes Talos reports down (by hostname) critical, the hops to them
    /// too, and such a node as the problem when the graph has none deeper.
    func marking(down hostnames: Set<String>) -> NetMap {
        let down = Set(
            nodes.filter { $0.kind == .node && hostnames.contains($0.name) && $0.health != .critical }.map(\.id))
        guard !down.isEmpty else { return self }
        let marked = nodes.map { down.contains($0.id) ? $0.with(health: .critical, detail: "NotReady") : $0 }
        let hops = edges.map { down.contains($0.to) ? NetMapEdge(from: $0.from, to: $0.to, health: .critical) : $0 }
        let first = marked.first { down.contains($0.id) }
        let keep = problem.map { $0.kind == .node && $0.detail != "SchedulingDisabled" } ?? false
        let cause = keep ? problem : first.map { NetMapProblem(kind: .node, name: $0.name, detail: "NotReady") }
        return NetMap(nodes: marked, edges: hops, problem: cause)
    }

    /// The box with id, nil when there is none.
    func node(_ id: String) -> NetMapNode? { nodes.first { $0.id == id } }

    /// A box's id as drawn: a folded one's is its column's "+N more" box.
    private func proxy(hiding hidden: Set<String>) -> (String) -> String {
        let layers = Dictionary(nodes.map { ($0.id, $0.layer) }, uniquingKeysWith: { first, _ in first })
        return { id in
            guard hidden.contains(id), let layer = layers[id] else { return id }
            return NetMap.moreID(layer: layer)
        }
    }

    /// From id, following the hops whose `next` end is reached, through their `step` end.
    private func walk(
        from id: String, next: (NetMapEdge) -> String,
        step: (NetMapEdge) -> String
    ) -> (nodes: Set<String>, edges: [NetMapEdge]) {
        var seen: Set<String> = [id]
        var lit: [NetMapEdge] = []
        var queue = [id]
        while let current = queue.popLast() {
            for edge in edges where next(edge) == current {
                lit.append(edge)
                if seen.insert(step(edge)).inserted { queue.append(step(edge)) }
            }
        }
        return (seen, lit)
    }
}
