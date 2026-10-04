import Foundation

// Mirrors go/ichorgo/kube_netpol*.go: the cluster's network policies, Kubernetes and Cilium
// ones alike, as the app shows them (who they apply to, whether they isolate it, what each rule
// lets through), and the pure logic the Network policies screen lists and words them with.

public enum NetPolicyKind: String, Sendable, CaseIterable {
    case networkPolicy = "NetworkPolicy"
    case cilium = "CiliumNetworkPolicy"
    case ciliumClusterwide = "CiliumClusterwideNetworkPolicy"
    case unknown = ""

    public init(wire: String) { self = NetPolicyKind(rawValue: wire) ?? .unknown }

    /// The badge: NP, CNP, CCNP.
    public var short: String {
        switch self {
        case .networkPolicy: "NP"
        case .cilium: "CNP"
        case .ciliumClusterwide: "CCNP"
        case .unknown: "?"
        }
    }
}

/// A traffic direction, as policies and flows name it.
public enum NetDirection: String, Sendable, CaseIterable {
    case ingress = "INGRESS"
    case egress = "EGRESS"
}

/// The policies of the cluster (os:admin), any CNI.
public struct NetPolicyReport: Decodable, Equatable, Sendable {
    /// The Cilium policy CRDs are served.
    public let cilium: Bool
    public let policies: [NetPolicy]
    public let namespaces: [NetPolicyNamespace]
    /// A kind that could not be read, "" otherwise.
    public let error: String

    public init(cilium: Bool = false, policies: [NetPolicy] = [], namespaces: [NetPolicyNamespace] = [], error: String = "") {
        self.cilium = cilium
        self.policies = policies
        self.namespaces = namespaces
        self.error = error
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        cilium = try c.field(.cilium, false)
        policies = try c.field(.policies, [])
        namespaces = try c.field(.namespaces, [])
        error = try c.field(.error, "")
    }

    private enum CodingKeys: String, CodingKey { case cilium, policies, namespaces, error }
}

/// Isolation of a namespace's pods in one direction.
public enum NetIsolation: Sendable {
    /// Every pod: only what policies allow passes.
    case full
    case partial
    case open
}

/// One namespace: its pods, how many are isolated per direction, its own policies.
public struct NetPolicyNamespace: Decodable, Equatable, Identifiable, Sendable {
    public let namespace: String
    public let pods: Int
    public let ingressIsolated: Int
    public let egressIsolated: Int
    public let policies: Int

    public var id: String { namespace }

    public init(namespace: String, pods: Int, ingressIsolated: Int = 0, egressIsolated: Int = 0, policies: Int = 0) {
        self.namespace = namespace
        self.pods = pods
        self.ingressIsolated = ingressIsolated
        self.egressIsolated = egressIsolated
        self.policies = policies
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        pods = try c.field(.pods, 0)
        ingressIsolated = try c.field(.ingressIsolated, 0)
        egressIsolated = try c.field(.egressIsolated, 0)
        policies = try c.field(.policies, 0)
    }

    public func isolation(_ direction: NetDirection) -> NetIsolation {
        let isolated = direction == .ingress ? ingressIsolated : egressIsolated
        if isolated == 0 || pods == 0 { return .open }
        return isolated >= pods ? .full : .partial
    }

    private enum CodingKeys: String, CodingKey { case namespace, pods, ingressIsolated, egressIsolated, policies }
}

/// What names a policy: a drop's attribution, the key to find it again.
public struct NetPolicyRef: Codable, Equatable, Hashable, Sendable {
    public let kind: String
    public let namespace: String
    public let name: String

    public init(kind: String, namespace: String = "", name: String) {
        self.kind = kind
        self.namespace = namespace
        self.name = name
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        kind = try c.field(.kind, "")
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
    }

    public func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(kind, forKey: .kind)
        try c.encodeIfPresent(Optional(namespace).nonEmpty, forKey: .namespace)
        try c.encode(name, forKey: .name)
    }

    /// "namespace/name", or the name of a cluster-wide policy.
    public var path: String { namespace.isEmpty ? name : "\(namespace)/\(name)" }

    private enum CodingKeys: String, CodingKey { case kind, namespace, name }
}

/// A network policy, whatever its kind.
public struct NetPolicy: Decodable, Equatable, Identifiable, Sendable {
    public let kind: NetPolicyKind
    /// The kind as Go named it, to match a drop's attribution.
    public let kindName: String
    /// "" for a cluster-wide policy.
    public let namespace: String
    public let name: String
    /// Unix ms.
    public let created: Int64
    /// Who the policy applies to: "" = every pod (of the namespace), else a selector.
    public let subject: String
    /// A cluster-wide policy pinned to a namespace.
    public let subjectNamespace: String
    /// A Cilium host policy: the subject selects nodes.
    public let nodes: Bool
    public let description: String
    /// The selected pods are isolated in that direction: only what the rules allow passes.
    public let ingress: Bool
    public let egress: Bool
    public let ingressRules: [NetRule]
    public let egressRules: [NetRule]
    /// The pods it selects now ("namespace/name"), at most 50, and their count.
    public let pods: [String]
    public let podCount: Int

    public var id: String { "\(kindName)/\(namespace)/\(name)" }
    public var ref: NetPolicyRef { NetPolicyRef(kind: kindName, namespace: namespace, name: name) }
    public var clusterWide: Bool { namespace.isEmpty }

    public init(kind: NetPolicyKind, namespace: String = "", name: String, created: Int64 = 0, subject: String = "",
                subjectNamespace: String = "", nodes: Bool = false, description: String = "",
                ingress: Bool = false, egress: Bool = false, ingressRules: [NetRule] = [], egressRules: [NetRule] = [],
                pods: [String] = [], podCount: Int = 0) {
        self.kind = kind
        kindName = kind.rawValue
        self.namespace = namespace
        self.name = name
        self.created = created
        self.subject = subject
        self.subjectNamespace = subjectNamespace
        self.nodes = nodes
        self.description = description
        self.ingress = ingress
        self.egress = egress
        self.ingressRules = ingressRules
        self.egressRules = egressRules
        self.pods = pods
        self.podCount = podCount
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        kindName = try c.field(.kind, "")
        kind = NetPolicyKind(wire: kindName)
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        created = try c.field(.created, 0)
        subject = try c.field(.subject, "")
        subjectNamespace = try c.field(.subjectNamespace, "")
        nodes = try c.field(.nodes, false)
        description = try c.field(.description, "")
        ingress = try c.field(.ingress, false)
        egress = try c.field(.egress, false)
        ingressRules = try c.field(.ingressRules, [])
        egressRules = try c.field(.egressRules, [])
        pods = try c.field(.pods, [])
        podCount = try c.field(.podCount, 0)
    }

    public func isolates(_ direction: NetDirection) -> Bool { direction == .ingress ? ingress : egress }

    public func rules(_ direction: NetDirection) -> [NetRule] { direction == .ingress ? ingressRules : egressRules }

    /// What the policy does to one direction of its pods.
    public func effect(_ direction: NetDirection) -> NetPolicyEffect {
        let rules = rules(direction)
        if isolates(direction) { return rules.isEmpty ? .denyAll : .isolated }
        return rules.isEmpty ? .open : .rulesOnly
    }

    /// Who it applies to, in words the app completes (see NetSubject).
    public var subjectScope: NetSubject {
        if nodes { return .nodes(subject) }
        if subject.isEmpty { return clusterWide && subjectNamespace.isEmpty ? .allPodsOfCluster : .allPods }
        return .selector(subject)
    }

    private enum CodingKeys: String, CodingKey {
        case kind, namespace, name, created, subject, subjectNamespace, nodes, description
        case ingress, egress, ingressRules, egressRules, pods, podCount
    }
}

/// Who a policy applies to.
public enum NetSubject: Equatable, Sendable {
    /// Every pod of its namespace.
    case allPods
    /// A cluster-wide policy on every pod.
    case allPodsOfCluster
    case selector(String)
    /// A host policy: "" = every node.
    case nodes(String)
}

/// What a policy does to one direction of its pods.
public enum NetPolicyEffect: Sendable {
    /// Isolated: only its rules (with other policies') let traffic through.
    case isolated
    /// Isolated with no rule at all: nothing passes unless another policy allows it.
    case denyAll
    /// Not isolated, yet with rules (e.g. Cilium deny rules, or a direction left open).
    case rulesOnly
    case open
}

/// One rule: traffic from/to any of its peers on any of its ports passes, or is denied.
public struct NetRule: Decodable, Equatable, Sendable {
    public let deny: Bool
    /// None: any peer.
    public let peers: [NetPeer]
    /// None: any port.
    public let ports: [NetPort]
    /// "HTTP GET /api", "DNS *".
    public let l7: [String]

    public init(deny: Bool = false, peers: [NetPeer] = [], ports: [NetPort] = [], l7: [String] = []) {
        self.deny = deny
        self.peers = peers
        self.ports = ports
        self.l7 = l7
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        deny = try c.field(.deny, false)
        peers = try c.field(.peers, [])
        ports = try c.field(.ports, [])
        l7 = try c.field(.l7, [])
    }

    private enum CodingKeys: String, CodingKey { case deny, peers, ports, l7 }
}

public enum NetPeerKind: String, Sendable, CaseIterable {
    case pods, namespaces, cidr, entity, fqdn, service, nodes
    case unknown = ""

    public init(wire: String) { self = NetPeerKind(rawValue: wire) ?? .unknown }
}

/// Where the pods a peer selects live.
public enum NetNamespaceScope: Equatable, Sendable {
    /// The policy's own namespace.
    case own
    case any
    case named(String)
    /// The namespaces a selector picks.
    case matching(String)
}

/// One side of a rule.
public struct NetPeer: Decodable, Equatable, Sendable {
    public let kind: NetPeerKind
    public let namespace: String
    public let namespaceSelector: String
    public let selector: String
    public let value: String
    public let except: [String]

    public init(kind: NetPeerKind, namespace: String = "", namespaceSelector: String = "", selector: String = "",
                value: String = "", except: [String] = []) {
        self.kind = kind
        self.namespace = namespace
        self.namespaceSelector = namespaceSelector
        self.selector = selector
        self.value = value
        self.except = except
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        kind = NetPeerKind(wire: try c.field(.kind, ""))
        namespace = try c.field(.namespace, "")
        namespaceSelector = try c.field(.namespaceSelector, "")
        selector = try c.field(.selector, "")
        value = try c.field(.value, "")
        except = try c.field(.except, [])
    }

    /// Where a pods peer's pods live; "" in a cluster-wide policy means any namespace.
    public func scope(clusterWide: Bool) -> NetNamespaceScope {
        if !namespaceSelector.isEmpty { return .matching(namespaceSelector) }
        switch namespace {
        case "*": return .any
        case "": return clusterWide ? .any : .own
        default: return .named(namespace)
        }
    }

    /// A service peer: "namespace/name", or its namespace and selector.
    public var serviceName: String {
        if !value.isEmpty { return value }
        return [namespace, selector].filter { !$0.isEmpty }.joined(separator: " ")
    }

    private enum CodingKeys: String, CodingKey { case kind, namespace, namespaceSelector, selector, value, except }
}

public struct NetPort: Decodable, Equatable, Sendable {
    /// TCP, UDP, SCTP, ANY, ICMPv4, ICMPv6.
    public let `protocol`: String
    /// A number or a named port; "" = every port.
    public let port: String
    public let endPort: Int

    public init(protocol: String, port: String = "", endPort: Int = 0) {
        self.protocol = `protocol`
        self.port = port
        self.endPort = endPort
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        `protocol` = try c.field(.protocol, "")
        port = try c.field(.port, "")
        endPort = try c.field(.endPort, 0)
    }

    /// "TCP 5432", "UDP 8000–8100", "53" (any protocol), "TCP" (every TCP port); "" when it
    /// names nothing, which the app words as any port.
    public var label: String {
        let proto = `protocol` == "ANY" ? "" : `protocol`
        var number = port
        if !number.isEmpty && endPort > 0, String(endPort) != number { number += "–\(endPort)" }
        return [proto, number].filter { !$0.isEmpty }.joined(separator: " ")
    }

    private enum CodingKeys: String, CodingKey { case `protocol`, port, endPort }
}

/// The policies of one section of the list: a namespace's, or the cluster-wide ones (nil).
public struct NetPolicySection: Equatable, Identifiable, Sendable {
    public let namespace: String?
    public let policies: [NetPolicy]

    public var id: String { namespace ?? "" }
}

/// How many policies of a kind the cluster has.
public struct NetPolicyKindCount: Hashable, Sendable {
    public let kind: NetPolicyKind
    public let count: Int
}

public extension NetPolicyReport {
    /// The policy a drop names, when it is still there.
    func policy(_ ref: NetPolicyRef) -> NetPolicy? {
        policies.first { $0.kindName == ref.kind && $0.namespace == ref.namespace && $0.name == ref.name }
    }

    /// Namespaces with at least one policy, sorted, for the filter.
    var policyNamespaces: [String] {
        Array(Set(policies.map(\.namespace).filter { !$0.isEmpty })).sorted()
    }

    /// The kinds present, with their count, in NP, CNP, CCNP order.
    var kindCounts: [NetPolicyKindCount] {
        NetPolicyKind.allCases.compactMap { kind in
            let count = policies.filter { $0.kind == kind }.count
            return count > 0 ? NetPolicyKindCount(kind: kind, count: count) : nil
        }
    }

    /// Policies by namespace (sorted), cluster-wide ones last; namespace keeps one namespace
    /// (and the cluster-wide ones, which may apply there too). query matches the name, the
    /// subject, the namespace or the description.
    func sections(namespace: String?, query: String) -> [NetPolicySection] {
        let q = query.trimmingCharacters(in: .whitespaces)
        let kept = policies.filter { p in
            (namespace == nil || p.namespace == namespace || p.clusterWide)
                && (q.isEmpty || [p.name, p.subject, p.namespace, p.description, p.kind.short]
                    .contains { $0.localizedCaseInsensitiveContains(q) })
        }
        let grouped = Dictionary(grouping: kept) { $0.namespace }
        let named = grouped.keys.filter { !$0.isEmpty }.sorted().map { ns in
            NetPolicySection(namespace: ns, policies: grouped[ns, default: []].sorted { $0.name < $1.name })
        }
        let clusterWide = grouped[""].map { [NetPolicySection(namespace: nil, policies: $0.sorted { $0.name < $1.name })] } ?? []
        return named + clusterWide
    }
}
