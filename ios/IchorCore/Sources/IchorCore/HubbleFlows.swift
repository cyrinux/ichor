import Foundation

// Mirrors go/ichorgo/kube_cilium.go and kube_hubble_*.go: whether Cilium and Hubble run, and
// the live flow view (recent flows, drops grouped with the policies behind them, agent states),
// with the pure logic the Live flows screen words them with.

/// Cilium in the cluster (os:admin).
public struct CiliumStatus: Decodable, Equatable, Sendable {
    public let installed: Bool
    public let namespace: String
    /// The agents' image tag.
    public let version: String
    public let hubble: Bool
    /// Flows each agent keeps.
    public let buffer: Int
    public let agents: [CiliumAgent]

    public init(installed: Bool = false, namespace: String = "", version: String = "", hubble: Bool = false,
                buffer: Int = 0, agents: [CiliumAgent] = []) {
        self.installed = installed
        self.namespace = namespace
        self.version = version
        self.hubble = hubble
        self.buffer = buffer
        self.agents = agents
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        installed = try c.field(.installed, false)
        namespace = try c.field(.namespace, "")
        version = try c.field(.version, "")
        hubble = try c.field(.hubble, false)
        buffer = try c.field(.buffer, 0)
        agents = try c.field(.agents, [])
    }

    private enum CodingKeys: String, CodingKey { case installed, namespace, version, hubble, buffer, agents }
}

public struct CiliumAgent: Decodable, Equatable, Sendable {
    public let node: String
    public let pod: String
    public let ready: Bool

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        node = try c.field(.node, "")
        pod = try c.field(.pod, "")
        ready = try c.field(.ready, false)
    }

    private enum CodingKeys: String, CodingKey { case node, pod, ready }
}

/// The live view, at most once a second while it changes.
public struct HubbleSnapshot: Decodable, Equatable, Sendable {
    public let namespace: String
    public let version: String
    public let buffer: Int
    public let seen: Int64
    public let dropped: Int64
    /// Events Hubble lost (ring buffer overrun).
    public let lost: Int64
    /// The policies could not be read: drops are not attributed.
    public let policiesError: String
    public let nodes: [HubbleAgentState]
    /// Newest first.
    public let flows: [HubbleFlow]
    /// Last seen first.
    public let drops: [HubbleDropGroup]

    public init(seen: Int64 = 0, dropped: Int64 = 0, lost: Int64 = 0, nodes: [HubbleAgentState] = [],
                flows: [HubbleFlow] = [], drops: [HubbleDropGroup] = []) {
        namespace = ""
        version = ""
        buffer = 0
        self.seen = seen
        self.dropped = dropped
        self.lost = lost
        policiesError = ""
        self.nodes = nodes
        self.flows = flows
        self.drops = drops
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        version = try c.field(.version, "")
        buffer = try c.field(.buffer, 0)
        seen = try c.field(.seen, 0)
        dropped = try c.field(.dropped, 0)
        lost = try c.field(.lost, 0)
        policiesError = try c.field(.policiesError, "")
        nodes = try c.field(.nodes, [])
        flows = try c.field(.flows, [])
        drops = try c.field(.drops, [])
    }

    private enum CodingKeys: String, CodingKey {
        case namespace, version, buffer, seen, dropped, lost, policiesError, nodes, flows, drops
    }
}

public enum HubbleAgentPhase: String, Sendable {
    case connecting, live, error
}

/// One cilium-agent the view follows.
public struct HubbleAgentState: Decodable, Equatable, Identifiable, Sendable {
    public let node: String
    public let pod: String
    public let state: HubbleAgentPhase
    public let error: String
    public let flows: Int64

    public var id: String { pod.isEmpty ? node : pod }

    public init(node: String, pod: String = "", state: HubbleAgentPhase, error: String = "", flows: Int64 = 0) {
        self.node = node
        self.pod = pod
        self.state = state
        self.error = error
        self.flows = flows
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        node = try c.field(.node, "")
        pod = try c.field(.pod, "")
        state = HubbleAgentPhase(rawValue: try c.field(.state, "")) ?? .connecting
        error = try c.field(.error, "")
        flows = try c.field(.flows, 0)
    }

    private enum CodingKeys: String, CodingKey { case node, pod, state, error, flows }
}

public enum HubbleVerdict: String, Sendable, WireEnum {
    case forwarded = "FORWARDED"
    case dropped = "DROPPED"
    /// Would have been dropped: the policy is in audit mode.
    case audit = "AUDIT"
    case error = "ERROR"
    case other = ""

    public static let wireFallback: Self = .other
}

/// Why Cilium dropped a flow.
public enum DropReason: Equatable, Sendable, WireDecodable {
    /// No policy allows this traffic.
    case policyDenied
    /// An explicit deny rule matched.
    case policyDeny
    case authRequired
    case staleOrUnroutableIP
    case ctMapInsertionFailed
    /// Any other reason, humanised ("Unsupported L3 protocol").
    case other(String)
    case unspecified

    public init(wire: String) {
        switch wire {
        case "": self = .unspecified
        case "POLICY_DENIED": self = .policyDenied
        case "POLICY_DENY": self = .policyDeny
        case "AUTH_REQUIRED": self = .authRequired
        case "STALE_OR_UNROUTABLE_IP": self = .staleOrUnroutableIP
        case "CT_MAP_INSERTION_FAILED": self = .ctMapInsertionFailed
        default: self = .other(humanizedEnum(wire))
        }
    }
}

/// "UNSUPPORTED_L3_PROTOCOL" → "Unsupported L3 protocol"; a number Go could not name stays.
func humanizedEnum(_ wire: String) -> String {
    let words = wire.split(separator: "_").map { $0.lowercased() }
    guard let first = words.first else { return wire }
    let known: [String: String] = ["ip": "IP", "ipv4": "IPv4", "ipv6": "IPv6", "l3": "L3", "l4": "L4", "l7": "L7",
                                   "ct": "CT", "tcp": "TCP", "udp": "UDP", "icmp": "ICMP", "nat": "NAT", "dns": "DNS",
                                   "fib": "FIB", "mtu": "MTU", "vlan": "VLAN", "srv6": "SRv6", "lb": "LB"]
    let shown = words.map { known[$0] ?? $0 }
    let head = known[first] ?? first.prefix(1).uppercased() + first.dropFirst()
    return ([head] + shown.dropFirst()).joined(separator: " ")
}

/// One side of a flow.
public struct HubblePeer: Codable, Equatable, Hashable, Sendable {
    public let namespace: String
    public let pod: String
    public let workload: String
    public let identity: Int64
    public let ip: String
    /// DNS names Cilium knows for the IP.
    public let names: [String]
    /// world, host, remote-node, kube-apiserver…
    public let reserved: String

    public init(namespace: String = "", pod: String = "", workload: String = "", identity: Int64 = 0, ip: String = "",
                names: [String] = [], reserved: String = "") {
        self.namespace = namespace
        self.pod = pod
        self.workload = workload
        self.identity = identity
        self.ip = ip
        self.names = names
        self.reserved = reserved
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        pod = try c.field(.pod, "")
        workload = try c.field(.workload, "")
        identity = try c.field(.identity, 0)
        ip = try c.field(.ip, "")
        names = try c.field(.names, [])
        reserved = try c.field(.reserved, "")
    }

    public func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encodeIfPresent(namespace.nonEmpty, forKey: .namespace)
        try c.encodeIfPresent(pod.nonEmpty, forKey: .pod)
        try c.encodeIfPresent(workload.nonEmpty, forKey: .workload)
        if identity != 0 { try c.encode(identity, forKey: .identity) }
        try c.encodeIfPresent(ip.nonEmpty, forKey: .ip)
        if !names.isEmpty { try c.encode(names, forKey: .names) }
        try c.encodeIfPresent(reserved.nonEmpty, forKey: .reserved)
    }

    /// "namespace/pod", else a DNS name, the IP, the reserved identity ("world").
    public var label: String {
        if !pod.isEmpty { return namespace.isEmpty ? pod : "\(namespace)/\(pod)" }
        return names.first ?? ip.or(reserved.or(namespace.or("?")))
    }

    /// What a rule would name to allow it: "namespace/workload", a DNS name, an IP.
    public var ruleLabel: String {
        if !workload.isEmpty { return namespace.isEmpty ? workload : "\(namespace)/\(workload)" }
        return label
    }

    private enum CodingKeys: String, CodingKey { case namespace, pod, workload, identity, ip, names, reserved }
}

/// "TCP 5432", "UDP 53", "ICMPv4": what a flow's port reads as.
public func flowPortLabel(protocol proto: String, port: Int64) -> String {
    [proto, port > 0 ? String(port) : ""].filter { !$0.isEmpty }.joined(separator: " ")
}

/// One flow.
public struct HubbleFlow: Codable, Equatable, Sendable {
    /// Unix ms.
    public let time: Int64
    public let node: String
    public let verdictName: String
    public let reasonName: String
    public let directionName: String
    public let `protocol`: String
    /// Destination port.
    public let port: Int64
    /// TCP flags, "SYN,ACK".
    public let flags: String
    public let reply: Bool
    public let type: String
    /// "DNS query example.org. A", "HTTP GET /".
    public let l7: String
    public let source: HubblePeer
    public let destination: HubblePeer
    public let deniedBy: [NetPolicyRef]

    public var verdict: HubbleVerdict { HubbleVerdict(wire: verdictName) }
    public var reason: DropReason { DropReason(wire: reasonName) }
    public var direction: NetDirection? { NetDirection(rawValue: directionName) }
    public var portLabel: String { flowPortLabel(protocol: `protocol`, port: port) }

    public init(time: Int64 = 0, node: String = "", verdict: String, reason: String = "", direction: String = "",
                protocol: String = "", port: Int64 = 0, l7: String = "",
                source: HubblePeer = HubblePeer(), destination: HubblePeer = HubblePeer(), deniedBy: [NetPolicyRef] = []) {
        self.time = time
        self.node = node
        verdictName = verdict
        reasonName = reason
        directionName = direction
        self.protocol = `protocol`
        self.port = port
        flags = ""
        reply = false
        type = ""
        self.l7 = l7
        self.source = source
        self.destination = destination
        self.deniedBy = deniedBy
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        time = try c.field(.time, 0)
        node = try c.field(.node, "")
        verdictName = try c.field(.verdict, "")
        reasonName = try c.field(.reason, "")
        directionName = try c.field(.direction, "")
        `protocol` = try c.field(.protocol, "")
        port = try c.field(.port, 0)
        flags = try c.field(.flags, "")
        reply = try c.field(.reply, false)
        type = try c.field(.type, "")
        l7 = try c.field(.l7, "")
        source = try c.field(.source, HubblePeer())
        destination = try c.field(.destination, HubblePeer())
        deniedBy = try c.field(.deniedBy, [])
    }

    /// The same fields Go sent, empty ones left out: what "Copy JSON" copies.
    public func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(time, forKey: .time)
        try c.encode(node, forKey: .node)
        try c.encode(verdictName, forKey: .verdict)
        try c.encodeIfPresent(reasonName.nonEmpty, forKey: .reason)
        try c.encodeIfPresent(directionName.nonEmpty, forKey: .direction)
        try c.encodeIfPresent(`protocol`.nonEmpty, forKey: .protocol)
        if port > 0 { try c.encode(port, forKey: .port) }
        try c.encodeIfPresent(flags.nonEmpty, forKey: .flags)
        if reply { try c.encode(reply, forKey: .reply) }
        try c.encodeIfPresent(type.nonEmpty, forKey: .type)
        try c.encodeIfPresent(l7.nonEmpty, forKey: .l7)
        try c.encode(source, forKey: .source)
        try c.encode(destination, forKey: .destination)
        if !deniedBy.isEmpty { try c.encode(deniedBy, forKey: .deniedBy) }
    }

    /// Pretty JSON with sorted keys.
    public var json: String {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.prettyPrinted, .sortedKeys, .withoutEscapingSlashes]
        return (try? encoder.encode(self)).map { String(decoding: $0, as: UTF8.self) } ?? "{}"
    }

    private enum CodingKeys: String, CodingKey {
        case time, node, verdict, reason, direction, `protocol`, port, flags, reply, type, l7, source, destination, deniedBy
    }
}

/// Flows dropped (or audited) alike: same peers, port, direction, verdict and reason.
public struct HubbleDropGroup: Decodable, Equatable, Identifiable, Sendable {
    public let source: HubblePeer
    public let destination: HubblePeer
    public let `protocol`: String
    public let port: Int64
    public let directionName: String
    public let verdictName: String
    public let reasonName: String
    public let count: Int64
    public let firstSeen: Int64
    public let lastSeen: Int64
    public let nodes: [String]
    /// What the flow names: explicit deny rules.
    public let deniedBy: [NetPolicyRef]
    /// What put the endpoint in default-deny.
    public let isolating: [NetPolicyRef]
    public let sample: HubbleFlow

    public var id: String {
        [source.label, destination.label, `protocol`, String(port), directionName, verdictName, reasonName].joined(separator: "|")
    }

    public var verdict: HubbleVerdict { HubbleVerdict(wire: verdictName) }
    public var reason: DropReason { DropReason(wire: reasonName) }
    public var direction: NetDirection? { NetDirection(rawValue: directionName) }
    public var portLabel: String { flowPortLabel(protocol: `protocol`, port: port) }

    /// ":5432/TCP", ":53/UDP", "/ICMPv4".
    public var endpointSuffix: String {
        let proto = `protocol`.isEmpty ? "" : "/\(`protocol`)"
        return port > 0 ? ":\(port)\(proto)" : proto
    }

    public init(source: HubblePeer, destination: HubblePeer, protocol: String = "", port: Int64 = 0, direction: String = "",
                verdict: String = "DROPPED", reason: String = "", count: Int64 = 1, lastSeen: Int64 = 0,
                deniedBy: [NetPolicyRef] = [], isolating: [NetPolicyRef] = []) {
        self.source = source
        self.destination = destination
        self.protocol = `protocol`
        self.port = port
        directionName = direction
        verdictName = verdict
        reasonName = reason
        self.count = count
        firstSeen = lastSeen
        self.lastSeen = lastSeen
        nodes = []
        self.deniedBy = deniedBy
        self.isolating = isolating
        sample = HubbleFlow(verdict: verdict, reason: reason, direction: direction, protocol: `protocol`, port: port,
                            source: source, destination: destination, deniedBy: deniedBy)
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        source = try c.field(.source, HubblePeer())
        destination = try c.field(.destination, HubblePeer())
        `protocol` = try c.field(.protocol, "")
        port = try c.field(.port, 0)
        directionName = try c.field(.direction, "")
        verdictName = try c.field(.verdict, "")
        reasonName = try c.field(.reason, "")
        count = try c.field(.count, 0)
        firstSeen = try c.field(.firstSeen, 0)
        lastSeen = try c.field(.lastSeen, 0)
        nodes = try c.field(.nodes, [])
        deniedBy = try c.field(.deniedBy, [])
        isolating = try c.field(.isolating, [])
        sample = try c.field(.sample, HubbleFlow(verdict: verdictName))
    }

    /// What to change to let the traffic through, when the policies behind the drop are known.
    public var hint: DropHint? {
        let port = portLabel
        if !deniedBy.isEmpty { return .removeDeny(policies: deniedBy) }
        guard !isolating.isEmpty, let direction else { return nil }
        switch direction {
        case .ingress: return .allowIngress(from: source.ruleLabel, port: port, policies: isolating)
        case .egress: return .allowEgress(to: destination.ruleLabel, port: port, policies: isolating)
        }
    }

    private enum CodingKeys: String, CodingKey {
        case source, destination, `protocol`, port, direction, verdict, reason, count, firstSeen, lastSeen
        case nodes, deniedBy, isolating, sample
    }
}

/// The sentence under a drop's policies; port is "" for a flow without one.
public enum DropHint: Equatable, Sendable {
    /// Add an ingress rule allowing `from` on `port` to one of the policies.
    case allowIngress(from: String, port: String, policies: [NetPolicyRef])
    /// Add an egress rule allowing `to` on `port` to one of the policies.
    case allowEgress(to: String, port: String, policies: [NetPolicyRef])
    /// An explicit deny rule wins over any allow: narrow or remove it.
    case removeDeny(policies: [NetPolicyRef])
}

/// The live view's filter: changing it restarts the stream.
public struct HubbleFilter: Equatable, Hashable, Sendable {
    public var namespace: String?
    /// Needs namespace.
    public var pod: String?
    public var dropsOnly: Bool

    public init(namespace: String? = nil, pod: String? = nil, dropsOnly: Bool = false) {
        self.namespace = namespace
        self.pod = namespace == nil ? nil : pod
        self.dropsOnly = dropsOnly
    }

    /// What Go takes: "" for no filter; a pod without a namespace is dropped.
    public var wire: (namespace: String, pod: String) {
        guard let namespace, !namespace.isEmpty else { return ("", "") }
        return (namespace, pod ?? "")
    }
}

public extension HubbleSnapshot {
    /// Namespaces the flows so far went from or to, sorted, for the filter.
    var flowNamespaces: [String] {
        let peers = flows.flatMap { [$0.source, $0.destination] } + drops.flatMap { [$0.source, $0.destination] }
        return Array(Set(peers.map(\.namespace).filter { !$0.isEmpty })).sorted()
    }

    /// Agents not live yet or failing first, so a problem shows at the start of the row.
    var agentsByAttention: [HubbleAgentState] {
        let rank: (HubbleAgentPhase) -> Int = { $0 == .error ? 0 : $0 == .connecting ? 1 : 2 }
        return nodes.enumerated().sorted { a, b in
            rank(a.element.state) != rank(b.element.state) ? rank(a.element.state) < rank(b.element.state) : a.offset < b.offset
        }.map(\.element)
    }
}
