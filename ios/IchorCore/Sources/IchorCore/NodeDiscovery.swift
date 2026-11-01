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

    /// The missing members not set aside with `dismissed` (any of their addresses, or their hostname).
    public func offer(dismissed: Set<String>) -> [DiscoveredNode] {
        missing.filter { node in
            !dismissed.contains(node.address) && !dismissed.contains(node.hostname)
                && !node.addresses.contains(where: dismissed.contains)
        }
    }
}

/// Whether a node can leave the talosconfig context; `endpoint`: the app also connects through it.
public struct ContextNodeRemoval: Decodable, Equatable, Sendable {
    public let allowed: Bool
    public let reason: String
    public let endpoint: Bool

    public init(allowed: Bool = false, reason: String = "", endpoint: Bool = false) {
        self.allowed = allowed
        self.reason = reason
        self.endpoint = endpoint
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        allowed = try c.field(.allowed, false)
        reason = try c.field(.reason, "")
        endpoint = try c.field(.endpoint, false)
    }

    private enum CodingKeys: String, CodingKey { case allowed, reason, endpoint }
}

/// Whether a node can be offered "Forget node": it does not answer and the cluster no longer lists it.
public struct NodeForgetCheck: Decodable, Equatable, Sendable {
    public let forgettable: Bool

    public init(forgettable: Bool = false) {
        self.forgettable = forgettable
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        forgettable = try c.field(.forgettable, false)
    }

    private enum CodingKeys: String, CodingKey { case forgettable }
}

/// One cluster member; `known` when the context already targets it (by address or hostname).
public struct DiscoveredNode: Decodable, Equatable, Hashable, Identifiable, Sendable {
    public let address: String
    public let addresses: [String]
    public let hostname: String
    public let role: String
    public let known: Bool

    public var id: String { address }

    public init(
        address: String, addresses: [String] = [], hostname: String = "", role: String = "", known: Bool = false
    ) {
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
