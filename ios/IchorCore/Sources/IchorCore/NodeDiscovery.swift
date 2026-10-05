import Foundation

/// The cluster's members as Talos cluster discovery knows them (`talosctl get members`).
public struct NodeDiscovery: Decodable, Equatable, Sendable {
    public let context: String
    public let nodes: [DiscoveredNode]

    public init(context: String, nodes: [DiscoveredNode]) {
        self.context = context
        self.nodes = nodes
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        context = try c.decode(String.self, forKey: .context)
        nodes = try c.field(.nodes, [])
    }

    private enum CodingKeys: String, CodingKey { case context, nodes }

    /// Members the talosconfig context does not target yet, with an address to add.
    public var missing: [DiscoveredNode] { nodes.filter { !$0.known && !$0.address.isEmpty } }

    /// The missing members not set aside with `dismissed` (their addresses).
    public func offer(dismissed: Set<String>) -> [DiscoveredNode] {
        missing.filter { !dismissed.contains($0.address) }
    }
}

/// One cluster member; `known` when the context already targets it (by address or hostname).
public struct DiscoveredNode: Decodable, Equatable, Hashable, Identifiable, Sendable {
    public let address: String
    public let addresses: [String]
    public let hostname: String
    public let role: String
    public let known: Bool

    public var id: String { address }

    public init(address: String, addresses: [String] = [], hostname: String = "", role: String = "", known: Bool = false) {
        self.address = address
        self.addresses = addresses
        self.hostname = hostname
        self.role = role
        self.known = known
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        address = try c.field(.address, "")
        addresses = try c.field(.addresses, [])
        hostname = try c.field(.hostname, "")
        role = try c.field(.role, "")
        known = try c.field(.known, false)
    }

    private enum CodingKeys: String, CodingKey { case address, addresses, hostname, role, known }
}
