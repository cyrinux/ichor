import Foundation

// Mirrors go/ichorgo/upgradecluster.go (ClusterUpgradePlan, StartClusterUpgrade).

/// A node of the rolling upgrade, in the order it is upgraded.
public struct ClusterUpgradeNode: Decodable, Equatable, Identifiable, Sendable {
    public enum State: String, Sendable {
        case pending, running, done, failed
    }

    public let node: String
    public let hostname: String
    /// controlplane | worker
    public let role: String
    /// The Talos version it runs now.
    public let from: String
    public let image: String
    /// Go's state; an unknown one reads as pending.
    public let state: String
    /// The etcd leader: upgraded last of the control planes, after it hands the leadership over.
    public let leader: Bool
    public let blockers: [String]
    public let warnings: [String]

    public var id: String { node }
    public var name: String { hostname.isEmpty ? node : hostname }
    public var controlPlane: Bool { role == "controlplane" }
    public var nodeState: State { State(rawValue: state) ?? .pending }

    private enum CodingKeys: String, CodingKey { case node, hostname, role, from, image, state, leader, blockers, warnings }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        node = try c.decode(String.self, forKey: .node)
        hostname = try c.field(.hostname, "")
        role = try c.field(.role, "")
        from = try c.field(.from, "")
        image = try c.field(.image, "")
        state = try c.field(.state, "pending")
        leader = try c.field(.leader, false)
        blockers = try c.field(.blockers, [])
        warnings = try c.field(.warnings, [])
    }
}

/// What upgrading every node to `version` would do, nodes in their upgrade order.
public struct ClusterUpgradePlan: Decodable, Equatable, Sendable {
    public let version: String
    public let image: String
    public let nodes: [ClusterUpgradeNode]
    public let blockers: [String]
    public let warnings: [String]
    /// The upgrade drains nodes by itself (Talos v1.18+); otherwise the switch offers it.
    public let drain: Bool

    public var pending: [ClusterUpgradeNode] { nodes.filter { $0.nodeState != .done } }
    public var done: Int { nodes.count { $0.nodeState == .done } }
    /// Some nodes already run the version, others not: a roll was interrupted, starting continues it.
    public var continues: Bool { done > 0 && !pending.isEmpty }
    public var upToDate: Bool { !nodes.isEmpty && pending.isEmpty }

    /// Every blocker, the cluster's then each node's (named).
    public var allBlockers: [String] { blockers + nodes.flatMap { n in n.blockers.map { "\(n.name): \($0)" } } }
    /// Every warning, the cluster's then each node's (named).
    public var allWarnings: [String] { warnings + nodes.flatMap { n in n.warnings.map { "\(n.name): \($0)" } } }
    public var canStart: Bool { !upToDate && allBlockers.isEmpty }

    private enum CodingKeys: String, CodingKey { case version, image, nodes, blockers, warnings, drain }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        version = try c.field(.version, "")
        image = try c.field(.image, "")
        nodes = try c.field(.nodes, [])
        blockers = try c.field(.blockers, [])
        warnings = try c.field(.warnings, [])
        drain = try c.field(.drain, false)
    }
}

/// A step of the roll: the node at `index` being upgraded, a gate, a pause, or the end.
public struct ClusterUpgradeProgress: Decodable, Equatable, Sendable {
    public enum Phase: String, Sendable {
        case node, gate, paused, done
    }

    public let phase: String
    public let index: Int
    public let total: Int
    public let node: String
    public let hostname: String
    /// The node's own upgrade phase while the phase is `node`.
    public let nodePhase: String
    public let message: String
    public let nodes: [ClusterUpgradeNode]

    public var rollPhase: Phase { Phase(rawValue: phase) ?? .node }
    public var name: String { hostname.isEmpty ? node : hostname }

    private enum CodingKeys: String, CodingKey { case phase, index, total, node, hostname, nodePhase, message, nodes }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        phase = try c.field(.phase, "")
        index = try c.field(.index, 0)
        total = try c.field(.total, 0)
        node = try c.field(.node, "")
        hostname = try c.field(.hostname, "")
        nodePhase = try c.field(.nodePhase, "")
        message = try c.field(.message, "")
        nodes = try c.field(.nodes, [])
    }
}
