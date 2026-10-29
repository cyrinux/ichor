import Foundation

/// The Load balancing screen (Go `KubeLoadBalancing`): the controllers that give LoadBalancer
/// Services their address (MetalLB, Cilium's LB IPAM, kube-vip), their pools, MetalLB's
/// advertisements and BGP peers, kube-vip's ranges and leases, and every LoadBalancer Service
/// with why it waits. `controller` takes a Service without class, "" when none was found.
public struct KubeLoadBalancing: Decodable, Sendable {
    public let controller: String
    public let controllers: [String]
    public let pools: [LBPool]
    public let advertisements: [LBAdvertisement]
    public let bgpPeers: [LBBGPPeer]
    public let configErrors: [LBConfigError]
    public let services: [LBService]
    public let kubeVip: LBKubeVip?
    public let partialAccess: Bool

    public var pending: [LBService] { services.filter(\.pending) }
    public var isEmpty: Bool { controllers.isEmpty && services.isEmpty }

    private enum CodingKeys: String, CodingKey {
        case controller, controllers, pools, advertisements, bgpPeers, configErrors, services, kubeVip, partialAccess
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        controller = try c.field(.controller, "")
        controllers = try c.field(.controllers, [])
        pools = try c.field(.pools, [])
        advertisements = try c.field(.advertisements, [])
        bgpPeers = try c.field(.bgpPeers, [])
        configErrors = try c.field(.configErrors, [])
        services = try c.field(.services, [])
        kubeVip = try c.decodeIfPresent(LBKubeVip.self, forKey: .kubeVip)
        partialAccess = try c.field(.partialAccess, false)
    }
}

/// Why a LoadBalancer Service has no address, as the core names it; `.unknown` for a cause
/// this version does not know.
public enum LBCause: String, WireEnum, Sendable, CaseIterable {
    case noController, configError, controllerDown, noPool, poolNotMatched, poolExhausted, poolNotAdvertised, unknown
    public static let wireFallback = LBCause.unknown
}

/// A pool: `available` of `total` addresses left (`countsKnown`: from the pool's status).
public struct LBPool: Decodable, Hashable, Identifiable, Sendable {
    public let name: String
    public let controller: String
    public let addresses: [String]
    public let total: Int
    public let assigned: Int
    public let available: Int
    public let countsKnown: Bool
    public let autoAssign: Bool
    public let namespaces: [String]
    /// "l2:name" or "bgp:name"; empty when nothing advertises the pool (MetalLB).
    public let advertisedBy: [String]
    public let services: [String]
    public let level: StorageLevel

    public var id: String { "\(controller)/\(name)" }
    /// Share of the pool in use, 0 to 1.
    public var used: Double { total > 0 ? Double(max(total - available, 0)) / Double(total) : 0 }

    private enum CodingKeys: String, CodingKey {
        case name, controller, addresses, total, assigned, available, countsKnown, autoAssign, namespaces, advertisedBy,
            services, level
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        controller = try c.field(.controller, "")
        addresses = try c.field(.addresses, [])
        total = try c.field(.total, 0)
        assigned = try c.field(.assigned, 0)
        available = try c.field(.available, 0)
        countsKnown = try c.field(.countsKnown, false)
        autoAssign = try c.field(.autoAssign, true)
        namespaces = try c.field(.namespaces, [])
        advertisedBy = try c.field(.advertisedBy, [])
        services = try c.field(.services, [])
        level = try c.wire(.level)
    }
}

/// A MetalLB L2Advertisement (`kind` "l2") or BGPAdvertisement ("bgp"), or a
/// CiliumL2AnnouncementPolicy (`controller` "cilium"); `allPools` when it names none.
/// `inactive`: why a Cilium policy announces nothing (l2Disabled, noLoadBalancerIPs,
/// noService), empty when it works.
public struct LBAdvertisement: Decodable, Hashable, Identifiable, Sendable {
    public let name: String
    public let controller: String
    public let inactive: String
    public let kind: String
    public let pools: [String]
    public let allPools: Bool
    public let interfaces: [String]
    public let peers: [String]

    public var id: String { "\(controller)/\(kind)/\(name)" }

    private enum CodingKeys: String, CodingKey {
        case name, controller, inactive, kind, pools, allPools, interfaces, peers
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        controller = try c.field(.controller, "")
        inactive = try c.field(.inactive, "")
        kind = try c.field(.kind, "")
        pools = try c.field(.pools, [])
        allPools = try c.field(.allPools, false)
        interfaces = try c.field(.interfaces, [])
        peers = try c.field(.peers, [])
    }
}

/// A BGP peer; `sessionsKnown` is false when no speaker's metrics could be read (never "down" then).
public struct LBBGPPeer: Decodable, Hashable, Identifiable, Sendable {
    public let name: String
    public let peerAddress: String
    public let interface: String
    public let peerASN: Int64
    public let myASN: Int64
    public let dynamicASN: String
    public let sessions: [LBBGPSession]
    public let sessionsKnown: Bool
    public let level: StorageLevel

    public var id: String { name }

    private enum CodingKeys: String, CodingKey {
        case name, peerAddress, interface, peerASN, myASN, dynamicASN, sessions, sessionsKnown, level
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        peerAddress = try c.field(.peerAddress, "")
        interface = try c.field(.interface, "")
        peerASN = try c.field(.peerASN, 0)
        myASN = try c.field(.myASN, 0)
        dynamicASN = try c.field(.dynamicASN, "")
        sessions = try c.field(.sessions, [])
        sessionsKnown = try c.field(.sessionsKnown, false)
        level = try c.wire(.level)
    }
}

public struct LBBGPSession: Decodable, Hashable, Sendable {
    public let node: String
    public let up: Bool
}

public struct LBConfigError: Decodable, Hashable, Sendable {
    public let component: String
    public let node: String
    public let message: String

    private enum CodingKeys: String, CodingKey { case component, node, message }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        component = try c.field(.component, "")
        node = try c.field(.node, "")
        message = try c.field(.message, "")
    }
}

/// A LoadBalancer Service: its addresses, pool, announcing node and, while it waits, `cause`.
public struct LBService: Decodable, Hashable, Identifiable, Sendable {
    public let namespace: String
    public let name: String
    public let addresses: [String]
    public let pool: String
    public let pending: Bool
    public let cause: LBCause
    public let announcedBy: String
    /// When it was made, unix ms; 0 when unknown.
    public let since: Int64
    public let level: StorageLevel

    public var id: String { "\(namespace)/\(name)" }

    private enum CodingKeys: String, CodingKey {
        case namespace, name, addresses, pool, pending, cause, announcedBy, since, level
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        addresses = try c.field(.addresses, [])
        pool = try c.field(.pool, "")
        pending = try c.field(.pending, false)
        cause = try c.wire(.cause)
        announcedBy = try c.field(.announcedBy, "")
        since = try c.field(.since, 0)
        level = try c.wire(.level)
    }
}

/// What kube-vip announces: its address ranges by namespace ("global" for every one) and its leases.
public struct LBKubeVip: Decodable, Hashable, Sendable {
    public let ranges: [LBKubeVipRange]
    public let leases: [LBKubeVipLease]

    private enum CodingKeys: String, CodingKey { case ranges, leases }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        ranges = try c.field(.ranges, [])
        leases = try c.field(.leases, [])
    }
}

public struct LBKubeVipRange: Decodable, Hashable, Sendable {
    public let scope: String
    public let cidrs: [String]
}

public struct LBKubeVipLease: Decodable, Hashable, Sendable {
    public let name: String
    public let namespace: String
    public let holder: String
    /// Last renewal, unix ms; 0 when unknown.
    public let renewed: Int64

    private enum CodingKeys: String, CodingKey { case name, namespace, holder, renewed }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        namespace = try c.field(.namespace, "")
        holder = try c.field(.holder, "")
        renewed = try c.field(.renewed, 0)
    }
}
