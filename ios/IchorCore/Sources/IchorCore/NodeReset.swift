import Foundation

/// What resetting a node would wipe and leave (Go NodeResetPlan); any blocker forbids it.
public struct NodeResetPlan: Decodable, Equatable, Sendable {
    public struct Member: Decodable, Equatable, Sendable {
        public let id: String
        public let healthy: Bool

        public init(id: String, healthy: Bool = true) {
            self.id = id
            self.healthy = healthy
        }

        private enum CodingKeys: String, CodingKey { case id, healthy }

        public init(from decoder: Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            id = try c.field(.id, "")
            healthy = try c.field(.healthy, false)
        }
    }

    public let node: String
    public let hostname: String
    /// "controlplane" or "worker".
    public let role: String
    /// The node's etcd member; nil for a worker or a control plane outside etcd.
    public let etcdMember: Member?
    public let lastControlPlane: Bool
    public let blockers: [String]
    public let warnings: [String]
    /// The disks other than the system disk, as /dev paths.
    public let userDisks: [String]

    public init(node: String = "", hostname: String = "", role: String = "worker", etcdMember: Member? = nil,
                lastControlPlane: Bool = false, blockers: [String] = [], warnings: [String] = [], userDisks: [String] = []) {
        self.node = node
        self.hostname = hostname
        self.role = role
        self.etcdMember = etcdMember
        self.lastControlPlane = lastControlPlane
        self.blockers = blockers
        self.warnings = warnings
        self.userDisks = userDisks
    }

    private enum CodingKeys: String, CodingKey {
        case node, hostname, role, etcdMember, lastControlPlane, blockers, warnings, userDisks
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        node = try c.field(.node, "")
        hostname = try c.field(.hostname, "")
        role = try c.field(.role, "")
        etcdMember = try c.decodeIfPresent(Member.self, forKey: .etcdMember)
        lastControlPlane = try c.field(.lastControlPlane, false)
        blockers = try c.field(.blockers, [])
        warnings = try c.field(.warnings, [])
        userDisks = try c.field(.userDisks, [])
    }

    public var allowed: Bool { blockers.isEmpty }

    public var isControlPlane: Bool { role == "controlplane" }

    /// The wipe modes on offer: without user disks, "everything" is the system disk alone.
    public var wipeModes: [ResetWipe] { userDisks.isEmpty ? [.system] : ResetWipe.allCases }

    /// A reset that does not leave etcd first leaves a dead member the others must remove.
    public func leavesDeadMember(graceful: Bool) -> Bool { !graceful && etcdMember != nil }
}

/// What a reset wipes; the raw value is NodeReset's wipe argument.
public enum ResetWipe: String, CaseIterable, Sendable {
    /// The system disk and the user disks of the plan.
    case all
    /// The system disk only.
    case system
    /// The user disks of the plan only.
    case user
}
