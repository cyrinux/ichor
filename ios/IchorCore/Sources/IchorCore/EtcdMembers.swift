import Foundation

/// What removing an etcd member would leave behind (EtcdMemberPlan).
public struct EtcdMemberPlan: Decodable, Equatable, Sendable {
    public struct Member: Decodable, Equatable, Sendable {
        public let id: String
        public let hostname: String

        public init(id: String, hostname: String = "") {
            self.id = id
            self.hostname = hostname
        }

        private enum CodingKeys: String, CodingKey { case id, hostname }

        public init(from decoder: Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            id = try c.decodeIfPresent(String.self, forKey: .id) ?? ""
            hostname = try c.decodeIfPresent(String.self, forKey: .hostname) ?? ""
        }
    }

    public let member: Member
    /// Healthy voting members once this one is gone.
    public let healthyAfter: Int
    /// Voting members once this one is gone.
    public let membersAfter: Int
    /// Whether the remaining healthy members still reach quorum.
    public let keepsQuorum: Bool
    public let blockers: [String]
    public let warnings: [String]

    public init(member: Member, healthyAfter: Int = 0, membersAfter: Int = 0, keepsQuorum: Bool = true,
                blockers: [String] = [], warnings: [String] = []) {
        self.member = member
        self.healthyAfter = healthyAfter
        self.membersAfter = membersAfter
        self.keepsQuorum = keepsQuorum
        self.blockers = blockers
        self.warnings = warnings
    }

    private enum CodingKeys: String, CodingKey { case member, healthyAfter, membersAfter, quorumAfter, blockers, warnings }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        member = try c.decodeIfPresent(Member.self, forKey: .member) ?? Member(id: "")
        healthyAfter = try c.decodeIfPresent(Int.self, forKey: .healthyAfter) ?? 0
        membersAfter = try c.decodeIfPresent(Int.self, forKey: .membersAfter) ?? 0
        // Go's verdict (a bool); a count of members needed is compared with the healthy ones.
        if let kept = try? c.decodeIfPresent(Bool.self, forKey: .quorumAfter) {
            keepsQuorum = kept
        } else if let needed = try? c.decodeIfPresent(Int.self, forKey: .quorumAfter) {
            keepsQuorum = healthyAfter >= needed && healthyAfter > 0
        } else {
            keepsQuorum = false
        }
        blockers = try c.decodeIfPresent([String].self, forKey: .blockers) ?? []
        warnings = try c.decodeIfPresent([String].self, forKey: .warnings) ?? []
    }
}

/// The member that gave up leadership (EtcdForfeitLeadership); "" when Go names none.
public struct EtcdForfeitResult: Decodable, Equatable, Sendable {
    public let member: String

    public init(member: String) { self.member = member }

    private enum CodingKeys: String, CodingKey { case member }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        member = try c.decodeIfPresent(String.self, forKey: .member) ?? ""
    }
}

/// The removal sheet's state for a plan: any blocker forbids the removal; there is no force.
/// Same rule as Android.
public struct EtcdRemovalGate: Equatable, Sendable {
    public let plan: EtcdMemberPlan
    public let busy: Bool

    public init(plan: EtcdMemberPlan, busy: Bool = false) {
        self.plan = plan
        self.busy = busy
    }

    public var blocked: Bool { !plan.blockers.isEmpty }
    public var canRemove: Bool { !blocked && !busy && !plan.member.id.isEmpty && !confirmationName.isEmpty }

    /// What the user types to confirm: the member's hostname, its id when it has none.
    public var confirmationName: String {
        let hostname = plan.member.hostname.trimmingCharacters(in: .whitespaces)
        return hostname.isEmpty ? plan.member.id.trimmingCharacters(in: .whitespaces) : hostname
    }
}

/// A member row's actions, given the loaded statuses. Every listed member can be asked to
/// leave (one that is down is the usual one to remove), except the last one.
public struct EtcdMemberActions: Equatable, Sendable {
    /// The node to ask to give up leadership, nil when this member is not a reachable leader
    /// or has no one to hand over to.
    public let forfeitNode: String?
    public let canRemove: Bool

    public init(forfeitNode: String?, canRemove: Bool) {
        self.forfeitNode = forfeitNode
        self.canRemove = canRemove
    }
}

public func etcdMemberActions(memberId: String, etcd: EtcdOverview) -> EtcdMemberActions {
    let status = etcd.statuses.first { $0.memberId == memberId && $0.error == nil }
    let voters = etcd.members.filter { !$0.isLearner }.count
    let forfeit = (status?.isLeader ?? false) && voters > 1 ? status?.node : nil
    // The last member cannot leave: there would be no cluster left.
    return EtcdMemberActions(forfeitNode: forfeit, canRemove: !memberId.isEmpty && etcd.members.count > 1)
}

/// The node to ask for removing `memberId`: another member that answered (never the member
/// itself, which may be gone), one without errors when there is one, the leader last. Nil
/// when no other member is reachable.
public func etcdRemovalNode(memberId: String, statuses: [EtcdNodeStatus]) -> String? {
    let others = statuses.filter { $0.error == nil && !$0.memberId.isEmpty && $0.memberId != memberId }
    // A member that reports no errors of its own first.
    let healthy = others.filter(\.errors.isEmpty)
    let candidates = healthy.isEmpty ? others : healthy
    return (candidates.first { !$0.isLeader } ?? candidates.first)?.node
}

/// Whether what the user typed confirms a risky action on `token` (a hostname, or a member
/// id when there is no hostname). A blank token confirms nothing: an empty field must never
/// be enough.
public func typedConfirmationMatches(_ typed: String, token: String) -> Bool {
    let wanted = token.trimmingCharacters(in: .whitespaces)
    return !wanted.isEmpty && typed.trimmingCharacters(in: .whitespaces) == wanted
}
