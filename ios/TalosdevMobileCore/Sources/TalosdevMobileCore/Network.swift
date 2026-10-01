import Foundation

/// A node's network state from the Go core (NodeNetwork); each section is best-effort and
/// a failed one is named in `errors` (links, addresses, routes, resolvers, timeServers).
public struct NodeNetwork: Decodable, Equatable, Sendable {
    public let links: [NetLink]
    public let addresses: [NetAddress]
    public let routes: [NetRoute]
    public let resolvers: [String]
    public let timeServers: [String]
    public let errors: [String: String]

    public init(links: [NetLink] = [], addresses: [NetAddress] = [], routes: [NetRoute] = [],
                resolvers: [String] = [], timeServers: [String] = [], errors: [String: String] = [:]) {
        self.links = links
        self.addresses = addresses
        self.routes = routes
        self.resolvers = resolvers
        self.timeServers = timeServers
        self.errors = errors
    }

    private enum CodingKeys: String, CodingKey { case links, addresses, routes, resolvers, timeServers, errors }

    // Go encodes empty (nil) slices and maps as null.
    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        links = try c.decodeIfPresent([NetLink].self, forKey: .links) ?? []
        addresses = try c.decodeIfPresent([NetAddress].self, forKey: .addresses) ?? []
        routes = try c.decodeIfPresent([NetRoute].self, forKey: .routes) ?? []
        resolvers = try c.decodeIfPresent([String].self, forKey: .resolvers) ?? []
        timeServers = try c.decodeIfPresent([String].self, forKey: .timeServers) ?? []
        errors = try c.decodeIfPresent([String: String].self, forKey: .errors) ?? [:]
    }
}

public struct NetLink: Decodable, Equatable, Identifiable, Sendable {
    public let name: String
    /// ether, loopback…
    public let type: String
    /// bond, vlan, veth, wireguard… (empty for a physical link).
    public let kind: String
    public let state: String
    public let hardwareAddr: String
    public let mtu: UInt32
    /// 0 when unknown.
    public let speedMbit: Int
    /// Pod/CNI plumbing (veth, lxc*, cilium_*…).
    public let virtual: Bool

    public var id: String { name }
    public var isUp: Bool { state.lowercased() == "up" }

    public init(name: String, type: String = "ether", kind: String = "", state: String = "up",
                hardwareAddr: String = "", mtu: UInt32 = 1500, speedMbit: Int = 0, virtual: Bool = false) {
        self.name = name
        self.type = type
        self.kind = kind
        self.state = state
        self.hardwareAddr = hardwareAddr
        self.mtu = mtu
        self.speedMbit = speedMbit
        self.virtual = virtual
    }
}

public struct NetAddress: Decodable, Equatable, Identifiable, Sendable {
    /// Prefix, e.g. 192.168.1.2/24.
    public let address: String
    public let link: String
    /// inet4 | inet6
    public let family: String
    public let scope: String
    public let virtual: Bool

    public var id: String { "\(link)/\(address)" }

    public init(address: String, link: String, family: String = "inet4", scope: String = "global", virtual: Bool = false) {
        self.address = address
        self.link = link
        self.family = family
        self.scope = scope
        self.virtual = virtual
    }
}

public struct NetRoute: Decodable, Equatable, Sendable {
    /// "default" for a default route.
    public let destination: String
    public let gateway: String
    public let link: String
    public let metric: UInt32
    public let table: String
    public let family: String
    public let virtual: Bool

    public var isDefault: Bool { destination == "default" }

    public init(destination: String, gateway: String = "", link: String = "", metric: UInt32 = 0,
                table: String = "main", family: String = "inet4", virtual: Bool = false) {
        self.destination = destination
        self.gateway = gateway
        self.link = link
        self.metric = metric
        self.table = table
        self.family = family
        self.virtual = virtual
    }
}

/// The Go core lists physical links first; virtual ones only when asked for.
public func visibleLinks(_ links: [NetLink], showVirtual: Bool) -> [NetLink] {
    showVirtual ? links : links.filter { !$0.virtual }
}

public func visibleAddresses(_ addresses: [NetAddress], showVirtual: Bool) -> [NetAddress] {
    showVirtual ? addresses : addresses.filter { !$0.virtual }
}

/// Default routes are always kept, even through a virtual link.
public func visibleRoutes(_ routes: [NetRoute], showVirtual: Bool) -> [NetRoute] {
    showVirtual ? routes : routes.filter { !$0.virtual || $0.isDefault }
}

/// "10 Gb/s", "100 Mb/s", nil when the speed is unknown.
public func formatLinkSpeed(_ mbit: Int) -> String? {
    guard mbit > 0 else { return nil }
    if mbit >= 1000 && mbit % 1000 == 0 { return "\(mbit / 1000) Gb/s" }
    if mbit >= 1000 { return String(format: "%.1f Gb/s", Double(mbit) / 1000) }
    return "\(mbit) Mb/s"
}

/// A TCP/UDP socket on the node (NodeConnections), host network namespace.
public struct NodeConnection: Decodable, Equatable, Identifiable, Sendable {
    /// tcp, tcp6, udp, udp6
    public let `protocol`: String
    public let localIp: String
    public let localPort: UInt32
    public let remoteIp: String
    public let remotePort: UInt32
    /// LISTEN, ESTABLISHED…
    public let state: String
    public let listening: Bool
    public let pid: UInt32
    public let processName: String

    public var id: String { "\(`protocol`) \(localIp):\(localPort) \(remoteIp):\(remotePort) \(pid)" }

    public init(protocol proto: String = "tcp", localIp: String = "0.0.0.0", localPort: UInt32, remoteIp: String = "0.0.0.0",
                remotePort: UInt32 = 0, state: String = "LISTEN", listening: Bool = true, pid: UInt32 = 0, processName: String = "") {
        self.protocol = proto
        self.localIp = localIp
        self.localPort = localPort
        self.remoteIp = remoteIp
        self.remotePort = remotePort
        self.state = state
        self.listening = listening
        self.pid = pid
        self.processName = processName
    }

    private enum CodingKeys: String, CodingKey {
        case `protocol`, localIp, localPort, remoteIp, remotePort, state, listening, pid, processName
    }

    // pid and processName are omitted when the node did not resolve the owning process.
    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        self.protocol = try c.decodeIfPresent(String.self, forKey: .protocol) ?? ""
        localIp = try c.decodeIfPresent(String.self, forKey: .localIp) ?? ""
        localPort = try c.decodeIfPresent(UInt32.self, forKey: .localPort) ?? 0
        remoteIp = try c.decodeIfPresent(String.self, forKey: .remoteIp) ?? ""
        remotePort = try c.decodeIfPresent(UInt32.self, forKey: .remotePort) ?? 0
        state = try c.decodeIfPresent(String.self, forKey: .state) ?? ""
        listening = try c.decodeIfPresent(Bool.self, forKey: .listening) ?? false
        pid = try c.decodeIfPresent(UInt32.self, forKey: .pid) ?? 0
        processName = try c.decodeIfPresent(String.self, forKey: .processName) ?? ""
    }

    /// "10.0.0.2:6443", "[fe80::1]:22".
    public var localEndpoint: String { endpoint(localIp, localPort) }
    public var remoteEndpoint: String { endpoint(remoteIp, remotePort) }

    private func endpoint(_ ip: String, _ port: UInt32) -> String {
        ip.contains(":") ? "[\(ip)]:\(port)" : "\(ip):\(port)"
    }
}

public enum ConnectionFilter: String, CaseIterable, Sendable {
    case listening, all
}

/// Listening sockets only (or all), matching the query on addresses, ports, state, process
/// name or pid, case-insensitively; the Go core's order (listeners first, by port) is kept.
public func filterConnections(_ connections: [NodeConnection], filter: ConnectionFilter, query: String) -> [NodeConnection] {
    let needle = query.trimmingCharacters(in: .whitespaces)
    return connections.filter { c in
        if filter == .listening && !c.listening { return false }
        guard !needle.isEmpty else { return true }
        let fields = [c.localEndpoint, c.remoteEndpoint, c.state, c.processName, c.protocol, c.pid > 0 ? "\(c.pid)" : ""]
        return fields.contains { $0.range(of: needle, options: .caseInsensitive) != nil }
    }
}
