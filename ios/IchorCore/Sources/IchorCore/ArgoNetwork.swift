import Foundation

// Mirrors go/ichorgo/kube_argocd_network.go: how traffic reaches an Argo CD app, as a layered
// graph read left to right (host → Gateway → route → Service → pod → node), and the pure logic
// the Network section draws it with: its columns, the path through a tapped box, the pods
// folded behind "+N more", the problem to word.

/// The columns of the graph, left to right.
public enum ArgoNetLayer: Int, Sendable, CaseIterable, Comparable {
    case entry, gateway, route, service, pod, node

    public static func < (a: ArgoNetLayer, b: ArgoNetLayer) -> Bool { a.rawValue < b.rawValue }
}

public enum ArgoNetKind: String, Sendable, CaseIterable {
    case host = "Host"
    case loadBalancer = "LoadBalancer"
    case gateway = "Gateway"
    case ingress = "Ingress"
    case httpRoute = "HTTPRoute"
    case service = "Service"
    case pod = "Pod"
    case node = "Node"
    case unknown = ""

    public init(wire: String) { self = ArgoNetKind(rawValue: wire) ?? .unknown }

    /// A Gateway or a route, which a shared one (not the app's) can be.
    public var routesTraffic: Bool { self == .gateway || self == .ingress || self == .httpRoute }
}

/// The network view of one app (os:admin).
public struct ArgoNetwork: Decodable, Equatable, Sendable {
    /// By layer, worst first.
    public let nodes: [ArgoNetNode]
    public let edges: [ArgoNetEdge]
    /// The deepest broken box, the likely root cause; nil when every box is fine.
    public let problem: ArgoNetProblem?

    public init(nodes: [ArgoNetNode] = [], edges: [ArgoNetEdge] = [], problem: ArgoNetProblem? = nil) {
        self.nodes = nodes
        self.edges = edges
        self.problem = problem
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        nodes = try c.field(.nodes, [])
        edges = try c.field(.edges, [])
        problem = try c.decodeIfPresent(ArgoNetProblem.self, forKey: .problem)
    }

    private enum CodingKeys: String, CodingKey { case nodes, edges, problem }
}

/// One box: a host, a Gateway, a route, a Service, a pod or a node.
public struct ArgoNetNode: Decodable, Equatable, Identifiable, Sendable {
    public let id: String
    public let layer: ArgoNetLayer
    public let kind: ArgoNetKind
    public let namespace: String
    public let name: String
    /// "ClusterIP 10.96.0.12 · 80→8080", a pod's status, an address.
    public let detail: String
    public let health: ServiceHealth
    /// What a host opens, "" otherwise.
    public let url: String
    /// The app's own resource: false for a shared Gateway or route, pods and nodes.
    public let managed: Bool

    public init(id: String, layer: ArgoNetLayer, kind: ArgoNetKind, namespace: String = "", name: String,
                detail: String = "", health: ServiceHealth = .ok, url: String = "", managed: Bool = false) {
        self.id = id
        self.layer = layer
        self.kind = kind
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
        let wire = try c.field(.layer, 0)
        layer = ArgoNetLayer(rawValue: min(max(wire, 0), ArgoNetLayer.node.rawValue)) ?? .entry
        kind = ArgoNetKind(wire: try c.field(.kind, ""))
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        detail = try c.field(.detail, "")
        health = ServiceHealth(wire: try c.field(.health, ""))
        url = try c.field(.url, "")
        managed = try c.field(.managed, false)
    }

    /// Shared with other apps: drawn with a dashed outline.
    public var shared: Bool { kind.routesTraffic && !managed }

    /// The same box with another health (a node Talos reports down).
    func with(health: ServiceHealth, detail: String) -> ArgoNetNode {
        ArgoNetNode(id: id, layer: layer, kind: kind, namespace: namespace, name: name, detail: detail,
                    health: health, url: url, managed: managed)
    }

    private enum CodingKeys: String, CodingKey { case id, layer, kind, namespace, name, detail, health, url, managed }
}

/// One hop, coloured by its target: traffic stops where it is red.
public struct ArgoNetEdge: Decodable, Equatable, Hashable, Identifiable, Sendable {
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
        health = ServiceHealth(wire: try c.field(.health, ""))
    }

    private enum CodingKeys: String, CodingKey { case from, to, health }
}

public struct ArgoNetProblem: Decodable, Equatable, Sendable {
    public let kind: ArgoNetKind
    public let namespace: String
    public let name: String
    public let detail: String

    public init(kind: ArgoNetKind, namespace: String = "", name: String, detail: String = "") {
        self.kind = kind
        self.namespace = namespace
        self.name = name
        self.detail = detail
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        kind = ArgoNetKind(wire: try c.field(.kind, ""))
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        detail = try c.field(.detail, "")
    }

    /// What the banner says, for the app to word.
    public var reason: ArgoNetReason {
        switch kind {
        case .node: detail == "SchedulingDisabled" ? .nodeCordoned(name) : .nodeNotReady(name)
        case .pod: .pod(name, detail)
        case .service: .serviceWithoutPods(name)
        case .ingress, .httpRoute, .gateway: .noHealthyBackend(kind.rawValue, name)
        case .host, .loadBalancer, .unknown: .other(kind.rawValue, name, detail)
        }
    }

    private enum CodingKeys: String, CodingKey { case kind, namespace, name, detail }
}

/// Why traffic does not get through, worded by the app.
public enum ArgoNetReason: Equatable, Sendable {
    case nodeNotReady(String)
    case nodeCordoned(String)
    case pod(String, String)
    case serviceWithoutPods(String)
    case noHealthyBackend(String, String)
    case other(String, String, String)
}

/// One column of the graph; hidden: pods folded behind "+N more".
public struct ArgoNetColumn: Equatable, Identifiable, Sendable {
    public let layer: ArgoNetLayer
    public let nodes: [ArgoNetNode]
    public let hidden: [ArgoNetNode]

    public var id: Int { layer.rawValue }
}

/// What a tapped box lights up: everything upstream and downstream of it.
public struct ArgoNetPath: Equatable, Sendable {
    public let nodes: Set<String>
    public let edges: Set<String>

    public func contains(node id: String) -> Bool { nodes.contains(id) }
    public func contains(edge id: String) -> Bool { edges.contains(id) }
}

public extension ArgoNetwork {
    /// The id of the "+N more" box the folded pods' hops are drawn to.
    static let morePodsID = "more/pods"
    /// Pods shown before the rest folds behind "+N more".
    static let podLimit = 8

    var isEmpty: Bool { nodes.isEmpty }

    /// The non-empty layers, left to right, each worst first (as sent). Beyond podLimit pods,
    /// the rest folds unless expanded.
    func columns(expanded: Bool = false, limit: Int = ArgoNetwork.podLimit) -> [ArgoNetColumn] {
        let byLayer = Dictionary(grouping: nodes, by: \.layer)
        return ArgoNetLayer.allCases.compactMap { layer in
            guard let members = byLayer[layer], !members.isEmpty else { return nil }
            guard layer == .pod, !expanded, members.count > limit else {
                return ArgoNetColumn(layer: layer, nodes: members, hidden: [])
            }
            return ArgoNetColumn(layer: layer, nodes: Array(members.prefix(limit)), hidden: Array(members.dropFirst(limit)))
        }
    }

    /// The hops to draw with the boxes of hidden folded: theirs go to morePodsID instead, once
    /// per pair, with the worst health of those it stands for.
    func edges(hiding hidden: Set<String>) -> [ArgoNetEdge] {
        guard !hidden.isEmpty else { return edges }
        var order: [String] = []
        var merged: [String: ArgoNetEdge] = [:]
        for edge in edges {
            let shown = ArgoNetEdge(from: hidden.contains(edge.from) ? Self.morePodsID : edge.from,
                                    to: hidden.contains(edge.to) ? Self.morePodsID : edge.to, health: edge.health)
            if let known = merged[shown.id] {
                merged[shown.id] = ArgoNetEdge(from: shown.from, to: shown.to, health: min(known.health, shown.health))
            } else {
                order.append(shown.id)
                merged[shown.id] = shown
            }
        }
        return order.compactMap { merged[$0] }
    }

    /// Every box and hop traffic through id takes: upstream to the hosts, downstream to the
    /// nodes. With boxes hidden, the "+N more" box and its hops light up for them.
    func path(through id: String, hiding hidden: Set<String> = []) -> ArgoNetPath {
        let upstream = walk(from: id, next: { edge in edge.to }, step: { edge in edge.from })
        let downstream = walk(from: id, next: { edge in edge.from }, step: { edge in edge.to })
        var nodes = upstream.nodes.union(downstream.nodes)
        let lit = upstream.edges + downstream.edges
        var edgeIDs = Set(lit.map(\.id))
        if !hidden.isEmpty {
            let map = { (node: String) in hidden.contains(node) ? Self.morePodsID : node }
            if !nodes.isDisjoint(with: hidden) { nodes.insert(Self.morePodsID) }
            edgeIDs.formUnion(lit.map { ArgoNetEdge(from: map($0.from), to: map($0.to)).id })
        }
        return ArgoNetPath(nodes: nodes, edges: edgeIDs)
    }

    /// The graph with the nodes Talos reports down (by hostname) critical, the hops to them
    /// too, and such a node as the problem when the graph has none deeper.
    func marking(down hostnames: Set<String>) -> ArgoNetwork {
        let down = Set(nodes.filter { $0.kind == .node && hostnames.contains($0.name) && $0.health != .critical }.map(\.id))
        guard !down.isEmpty else { return self }
        let marked = nodes.map { down.contains($0.id) ? $0.with(health: .critical, detail: "NotReady") : $0 }
        let hops = edges.map { down.contains($0.to) ? ArgoNetEdge(from: $0.from, to: $0.to, health: .critical) : $0 }
        let first = marked.first { down.contains($0.id) }
        let keep = problem.map { $0.kind == .node && $0.detail != "SchedulingDisabled" } ?? false
        let cause = keep ? problem : first.map { ArgoNetProblem(kind: .node, name: $0.name, detail: "NotReady") }
        return ArgoNetwork(nodes: marked, edges: hops, problem: cause)
    }

    /// The box with id, nil when there is none.
    func node(_ id: String) -> ArgoNetNode? { nodes.first { $0.id == id } }

    /// From id, following the hops whose `next` end is reached, through their `step` end.
    private func walk(from id: String, next: (ArgoNetEdge) -> String,
                      step: (ArgoNetEdge) -> String) -> (nodes: Set<String>, edges: [ArgoNetEdge]) {
        var seen: Set<String> = [id]
        var lit: [ArgoNetEdge] = []
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
