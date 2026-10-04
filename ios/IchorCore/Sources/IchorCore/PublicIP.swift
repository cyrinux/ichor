import Foundation

/// One Kubernetes node's answer to a public IP probe (a curl pod on it); `error` when none.
public struct PublicIPProbe: Codable, Equatable, Sendable {
    public let name: String
    /// Its InternalIP, to match it to a Talos node.
    public let address: String
    public let publicIP: String
    public let error: String

    public init(name: String, address: String = "", publicIP: String = "", error: String = "") {
        self.name = name
        self.address = address
        self.publicIP = publicIP
        self.error = error
    }

    private enum CodingKeys: String, CodingKey { case name, address, publicIP, error }

    // Go leaves empty fields out.
    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.decodeIfPresent(String.self, forKey: .name) ?? ""
        address = try c.decodeIfPresent(String.self, forKey: .address) ?? ""
        publicIP = try c.decodeIfPresent(String.self, forKey: .publicIP) ?? ""
        error = try c.decodeIfPresent(String.self, forKey: .error) ?? ""
    }
}

/// A public IP probe of every node, `at` epoch milliseconds.
public struct PublicIPReport: Codable, Equatable, Sendable {
    public let nodes: [PublicIPProbe]
    public let at: Int64

    public init(nodes: [PublicIPProbe] = [], at: Int64 = 0) {
        self.nodes = nodes
        self.at = at
    }

    private enum CodingKeys: String, CodingKey { case nodes, at }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        nodes = try c.decodeIfPresent([PublicIPProbe].self, forKey: .nodes) ?? []
        at = try c.decodeIfPresent(Int64.self, forKey: .at) ?? 0
    }

    /// The address found for `node`: by its talosconfig address, else its hostname.
    public func ip(for node: NodeOverview) -> String? {
        let found = nodes.filter { !$0.publicIP.isEmpty }
        if let byAddress = found.first(where: { $0.address == node.node }) { return byAddress.publicIP }
        let host = node.hostname
        guard !host.isEmpty, host != node.node else { return nil }
        let short = { (name: String) in String(name.lowercased().split(separator: ".").first ?? "") }
        return (found.first { $0.name.lowercased() == host.lowercased() }
            ?? found.first { short($0.name) == short(host) })?.publicIP
    }

    /// The first node a probe found nothing for, and why ("" for none).
    public var firstError: String {
        nodes.first { !$0.error.isEmpty }.map { "\($0.name): \($0.error)" } ?? ""
    }
}

public extension NodeOverview {
    /// What the nodes list shows: what Talos knows, else what a probe found.
    func shownPublicIPs(probed: PublicIPReport?) -> [String] {
        if !publicIPs.isEmpty { return publicIPs }
        return probed?.ip(for: self).map { [$0] } ?? []
    }
}

public extension ClusterOverview {
    /// Whether a probe could find more: a node that answers, with no public IP Talos knows.
    var lacksPublicIPs: Bool { nodes.contains { $0.reachable && $0.publicIPs.isEmpty } }
}
