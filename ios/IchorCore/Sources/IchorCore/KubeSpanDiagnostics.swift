import Foundation

// Mirrors go/ichorgo/kubespan_diag.go (KubeSpanDiagnosticsAll).

public struct KubeSpanDiagAll: Decodable, Equatable, Sendable {
    public let nodes: [KubeSpanDiag]

    public init(nodes: [KubeSpanDiag]) { self.nodes = nodes }

    private enum CodingKeys: String, CodingKey { case nodes }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        nodes = try c.field(.nodes, [])
    }

    /// `node`'s diagnosis, nil when it was not read.
    public func node(_ node: String) -> KubeSpanDiag? { nodes.first { $0.node == node } }

    /// The peer `publicKey` as `node` sees it.
    public func peer(node: String, publicKey: String) -> KubeSpanDiagPeer? {
        self.node(node)?.peers.first { $0.publicKey == publicKey }
    }
}

public struct KubeSpanDiag: Decodable, Equatable, Sendable {
    public struct Config: Decodable, Equatable, Sendable {
        public let enabled: Bool
        /// 0: Talos's default.
        public let mtu: Int
        public let endpointFilters: [String]

        private enum CodingKeys: String, CodingKey { case enabled, mtu, endpointFilters }

        public init(from decoder: Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            enabled = try c.field(.enabled, false)
            mtu = try c.field(.mtu, 0)
            endpointFilters = try c.field(.endpointFilters, [])
        }
    }

    public let node: String
    public let hostname: String
    public let config: Config?
    /// The kubespan link's MTU, 0 when the link is not there.
    public let linkMtu: Int
    public let peers: [KubeSpanDiagPeer]
    /// Set on a node Omni manages.
    public let siderolink: SiderolinkDiag?
    public let errors: [String: String]

    private enum CodingKeys: String, CodingKey { case node, hostname, config, linkMtu, peers, siderolink, errors }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        node = try c.field(.node, "")
        hostname = try c.field(.hostname, "")
        config = try c.decodeIfPresent(Config.self, forKey: .config)
        linkMtu = try c.field(.linkMtu, 0)
        peers = try c.field(.peers, [])
        siderolink = try c.decodeIfPresent(SiderolinkDiag.self, forKey: .siderolink)
        errors = try c.field(.errors, [:])
    }
}

public struct KubeSpanDiagPeer: Decodable, Equatable, Identifiable, Sendable {
    public struct Verdict: Decodable, Equatable, Hashable, Sendable {
        public let kind: String
        public let message: String
    }

    public let publicKey: String
    public let label: String
    public let state: String
    public let address: String
    public let allowedIPs: [String]
    public let endpointsTried: [String]
    public let endpoint: String
    public let lastUsedEndpoint: String
    /// Unix seconds, 0 = never.
    public let lastHandshake: Int64
    public let lastEndpointChange: Int64
    /// Why the link is not right, in the core's words; empty when nothing was found.
    public let verdicts: [Verdict]

    public var id: String { publicKey }

    private enum CodingKeys: String, CodingKey {
        case publicKey, label, state, address, allowedIPs, endpointsTried, endpoint, lastUsedEndpoint, lastHandshake, lastEndpointChange, verdicts
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        publicKey = try c.field(.publicKey, "")
        label = try c.field(.label, "")
        state = try c.field(.state, "")
        address = try c.field(.address, "")
        allowedIPs = try c.field(.allowedIPs, [])
        endpointsTried = try c.field(.endpointsTried, [])
        endpoint = try c.field(.endpoint, "")
        lastUsedEndpoint = try c.field(.lastUsedEndpoint, "")
        lastHandshake = try c.field(.lastHandshake, 0)
        lastEndpointChange = try c.field(.lastEndpointChange, 0)
        verdicts = try c.field(.verdicts, [])
    }
}

public struct SiderolinkDiag: Decodable, Equatable, Sendable {
    public let host: String
    public let connected: Bool
    public let grpcTunnel: Bool

    private enum CodingKeys: String, CodingKey { case host, connected, grpcTunnel }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        host = try c.field(.host, "")
        connected = try c.field(.connected, false)
        grpcTunnel = try c.field(.grpcTunnel, false)
    }
}
