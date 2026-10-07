import Foundation

// Mirrors go/ichorgo/topology_build.go (ClusterTopology), like Android's model/Topology.kt.

public struct ClusterTopology: Decodable, Equatable, Sendable {
    public let nodes: [TopologyNode]
    public let links: [TopologyLink]
    public let sites: [TopologySite]

    public init(nodes: [TopologyNode] = [], links: [TopologyLink] = [], sites: [TopologySite] = []) {
        self.nodes = nodes
        self.links = links
        self.sites = sites
    }

    private enum CodingKeys: String, CodingKey { case nodes, links, sites }

    // Go encodes empty (nil) slices as null.
    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        nodes = try c.field(.nodes, [])
        links = try c.field(.links, [])
        sites = try c.field(.sites, [])
    }

    /// Links the node is an end of that are down or degraded.
    public func brokenLinks(of nodeId: String) -> Int {
        links.filter { $0.isBroken && ($0.a == nodeId || $0.b == nodeId) }.count
    }
}

public struct TopologyNode: Decodable, Equatable, Identifiable, Sendable {
    /// Hostname: the key links and sites refer to.
    public let id: String
    /// The talosconfig target; empty when the node is only known through discovery or its peers.
    public let node: String
    public let hostname: String
    public let role: String
    public let addresses: [String]
    public let zone: String
    public let region: String
    /// ISO 3166-1 alpha-2 code guessed from the zone or region, empty when unknown.
    public let country: String
    public let site: String
    public let kubespan: Bool
    public let queried: Bool
    public let error: String?
    /// When an unreachable node last answered (epoch ms), kept by the app: see mergeLastKnown.
    public let lastSeen: Int64?

    public init(id: String, node: String = "", hostname: String = "", role: String = "", addresses: [String] = [],
                zone: String = "", region: String = "", country: String = "", site: String = "",
                kubespan: Bool = false, queried: Bool = false, error: String? = nil, lastSeen: Int64? = nil) {
        self.id = id
        self.node = node
        self.hostname = hostname
        self.role = role
        self.addresses = addresses
        self.zone = zone
        self.region = region
        self.country = country
        self.site = site
        self.kubespan = kubespan
        self.queried = queried
        self.error = error
        self.lastSeen = lastSeen
    }

    private enum CodingKeys: String, CodingKey {
        case id, node, hostname, role, addresses, zone, region, country, site, kubespan, queried, error
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        node = try c.field(.node, "")
        hostname = try c.field(.hostname, "")
        role = try c.field(.role, "")
        addresses = try c.field(.addresses, [])
        zone = try c.field(.zone, "")
        region = try c.field(.region, "")
        country = try c.field(.country, "")
        site = try c.field(.site, "")
        kubespan = try c.field(.kubespan, false)
        queried = try c.field(.queried, false)
        error = try c.decodeIfPresent(String.self, forKey: .error)
        lastSeen = nil
    }
}

public struct TopologySite: Decodable, Equatable, Hashable, Identifiable, Sendable {
    public let id: String
    /// Zone, shared private subnet, or empty.
    public let label: String
    /// "zone", "lan" or "node" (a node alone on its network).
    public let kind: String
    public let country: String
    public let nodes: [String]

    public init(id: String, label: String = "", kind: String = "", country: String = "", nodes: [String] = []) {
        self.id = id
        self.label = label
        self.kind = kind
        self.country = country
        self.nodes = nodes
    }

    private enum CodingKeys: String, CodingKey { case id, label, kind, country, nodes }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        label = try c.field(.label, "")
        kind = try c.field(.kind, "")
        country = try c.field(.country, "")
        nodes = try c.field(.nodes, [])
    }
}

public struct TopologyLink: Decodable, Equatable, Sendable {
    public let a: String
    public let b: String
    /// "up", "down", "degraded" (one end reports it down) or "unknown".
    public let state: String
    public let sides: [TopologyLinkSide]

    public init(a: String, b: String, state: String = "unknown", sides: [TopologyLinkSide] = []) {
        self.a = a
        self.b = b
        self.state = state
        self.sides = sides
    }

    private enum CodingKeys: String, CodingKey { case a, b, state, sides }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        a = try c.decode(String.self, forKey: .a)
        b = try c.decode(String.self, forKey: .b)
        state = try c.field(.state, "unknown")
        sides = try c.field(.sides, [])
    }

    public var isBroken: Bool { state == "down" || state == "degraded" }
}

public struct TopologyLinkSide: Decodable, Equatable, Sendable {
    public let from: String
    public let to: String
    public let state: String
    public let endpoint: String
    /// The endpoint is on a private network: both ends share a LAN.
    public let `private`: Bool
    public let rx: Int64
    public let tx: Int64
    public let lastHandshake: Int64

    private enum CodingKeys: String, CodingKey { case from, to, state, endpoint, `private`, rx, tx, lastHandshake }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        from = try c.decode(String.self, forKey: .from)
        to = try c.decode(String.self, forKey: .to)
        state = try c.field(.state, "unknown")
        endpoint = try c.field(.endpoint, "")
        `private` = try c.field(.`private`, false)
        rx = try c.field(.rx, 0)
        tx = try c.field(.tx, 0)
        lastHandshake = try c.field(.lastHandshake, 0)
    }
}

/// A run of the overview's nodes on one site of the map; `site` nil for nodes the map does not know.
public struct NodeGroup: Equatable, Identifiable, Sendable {
    public let site: TopologySite?
    public let nodes: [NodeOverview]

    public init(site: TopologySite?, nodes: [NodeOverview]) {
        self.site = site
        self.nodes = nodes
    }

    /// A key per group, for the site filter: the site's id, empty for the nodes the map misses.
    public var key: String { site?.id ?? "" }
    public var id: String { key }
}

/// `nodes` in the map's order: site by site as `topology.sites` lists them, control planes
/// first then by hostname within each, and the nodes the map misses last. Without a map (never
/// fetched, or nothing to place), one group with every node in that same order. Same as Android.
public func groupNodes(_ nodes: [NodeOverview], by topology: ClusterTopology?) -> [NodeGroup] {
    let ordered = nodes.overviewOrder
    let sites = topology?.sites ?? []
    if sites.isEmpty { return [NodeGroup(site: nil, nodes: ordered)] }
    // The map keys nodes by hostname; its talosconfig target is the fallback for a renamed node.
    let byId = Dictionary(sites.flatMap { site in site.nodes.map { ($0, site) } }, uniquingKeysWith: { _, last in last })
    let byTarget = Dictionary(
        (topology?.nodes ?? []).filter { !$0.node.trimmingCharacters(in: .whitespaces).isEmpty }
            .compactMap { n in byId[n.id].map { (n.node, $0) } },
        uniquingKeysWith: { _, last in last }
    )
    func siteOf(_ node: NodeOverview) -> TopologySite? { byId[node.hostname] ?? byTarget[node.node] }
    let placed = sites.compactMap { site -> NodeGroup? in
        let members = ordered.filter { siteOf($0) == site }
        return members.isEmpty ? nil : NodeGroup(site: site, nodes: members)
    }
    let rest = ordered.filter { siteOf($0) == nil }
    return rest.isEmpty ? placed : placed + [NodeGroup(site: nil, nodes: rest)]
}

/// "FR" -> 🇫🇷 (regional indicator symbols); "" for anything that is not two ASCII letters.
public func countryFlag(_ code: String) -> String {
    let letters = Array(code.uppercased().unicodeScalars)
    guard code.unicodeScalars.count == 2, letters.count == 2,
          letters.allSatisfy({ ("A"..."Z").contains($0) }) else { return "" }
    return String(String.UnicodeScalarView(letters.compactMap { Unicode.Scalar(regionalIndicatorA + $0.value - 65) }))
}

private let regionalIndicatorA: UInt32 = 0x1F1E6
