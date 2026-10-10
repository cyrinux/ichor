import Foundation

/// Where the guided replacement of a failed control plane stands (Go ControlPlaneReplacePlan),
/// read from the live cluster so that reopening the screen resumes.
public struct CpReplacePlan: Decodable, Equatable, Sendable {
    public struct Member: Decodable, Equatable, Sendable {
        public let id: String
        public let hostname: String
        public let node: String
        /// False once the member is removed from etcd.
        public let found: Bool
        public let healthy: Bool
        /// Whether the node's Talos API still answers (it can be reset).
        public let reachable: Bool

        public init(id: String = "", hostname: String = "", node: String = "", found: Bool = false,
                    healthy: Bool = false, reachable: Bool = false) {
            self.id = id
            self.hostname = hostname
            self.node = node
            self.found = found
            self.healthy = healthy
            self.reachable = reachable
        }

        private enum CodingKeys: String, CodingKey { case id, hostname, node, found, healthy, reachable }

        public init(from decoder: Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            id = try c.field(.id, "")
            hostname = try c.field(.hostname, "")
            node = try c.field(.node, "")
            found = try c.field(.found, false)
            healthy = try c.field(.healthy, false)
            reachable = try c.field(.reachable, false)
        }
    }

    public struct Quorum: Decodable, Equatable, Sendable {
        public let members: Int
        public let healthy: Int
        public let afterRemoval: Int
        public let healthyAfter: Int
        public let safe: Bool

        public init(members: Int = 0, healthy: Int = 0, afterRemoval: Int = 0, healthyAfter: Int = 0, safe: Bool = false) {
            self.members = members
            self.healthy = healthy
            self.afterRemoval = afterRemoval
            self.healthyAfter = healthyAfter
            self.safe = safe
        }

        private enum CodingKeys: String, CodingKey { case members, healthy, afterRemoval, healthyAfter, safe }

        public init(from decoder: Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            members = try c.field(.members, 0)
            healthy = try c.field(.healthy, 0)
            afterRemoval = try c.field(.afterRemoval, 0)
            healthyAfter = try c.field(.healthyAfter, 0)
            safe = try c.field(.safe, false)
        }
    }

    public struct Node: Decodable, Equatable, Sendable {
        public let id: String
        public let node: String
        public let hostname: String

        public init(id: String = "", node: String = "", hostname: String = "") {
            self.id = id
            self.node = node
            self.hostname = hostname
        }

        private enum CodingKeys: String, CodingKey { case id, node, hostname }

        public init(from decoder: Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            id = try c.field(.id, "")
            node = try c.field(.node, "")
            hostname = try c.field(.hostname, "")
        }
    }

    public struct Step: Decodable, Equatable, Sendable {
        public let id: String
        public let state: CpStepState
        public let detail: String

        public init(id: String, state: CpStepState, detail: String = "") {
            self.id = id
            self.state = state
            self.detail = detail
        }

        private enum CodingKeys: String, CodingKey { case id, state, detail }

        public init(from decoder: Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            id = try c.field(.id, "")
            // Unknown states read as pending: never enable an action Go did not mark ready.
            state = CpStepState(rawValue: try c.field(.state, "")) ?? .pending
            detail = try c.field(.detail, "")
        }
    }

    public let member: Member
    public let quorum: Quorum
    public let leader: Node
    public let steps: [Step]
    /// The healthy control plane whose machine config the new node copies; empty node: none.
    public let template: Node

    public init(member: Member = Member(), quorum: Quorum = Quorum(), leader: Node = Node(), steps: [Step] = [], template: Node = Node()) {
        self.member = member
        self.quorum = quorum
        self.leader = leader
        self.steps = steps
        self.template = template
    }

    private enum CodingKeys: String, CodingKey { case member, quorum, leader, steps, template }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        member = try c.field(.member, Member())
        quorum = try c.field(.quorum, Quorum())
        leader = try c.field(.leader, Node())
        steps = try c.field(.steps, [])
        template = try c.field(.template, Node())
    }

    /// The step `step`; pending when Go did not list it.
    public func step(_ step: CpStep) -> Step {
        steps.first { $0.id == step.rawValue } ?? Step(id: step.rawValue, state: .pending)
    }

    public func state(_ step: CpStep) -> CpStepState { self.step(step).state }

    /// What to type to confirm acting on the member: its hostname, its id when it has none.
    public var confirmationName: String { member.hostname.isEmpty ? member.id : member.hostname }

    /// The voting members to wait past for the new one: those left once the member is
    /// removed; a plan read after the removal already counts without it.
    public var membersBeforeJoin: Int { member.found ? quorum.afterRemoval : quorum.members }
}

/// The five steps of a replacement, in order.
public enum CpStep: String, CaseIterable, Sendable {
    case confirmQuorum, removeMember, resetOrPowerOff, bootNewNode, waitMember
}

public enum CpStepState: String, Sendable {
    case pending, ready, done, skipped, blocked
}

/// What Go ControlPlaneReplaceWait returns: joined once etcd has a new healthy voter.
public struct CpReplaceWait: Decodable, Equatable, Sendable {
    public let joined: Bool
    public let members: [CpReplacePlan.Member]
    public let detail: String

    private enum CodingKeys: String, CodingKey { case joined, members, detail }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        joined = try c.field(.joined, false)
        members = try c.field(.members, [])
        detail = try c.field(.detail, "")
    }
}

/// The line to run from a laptop on the new node, booted in maintenance mode.
public let applyConfigCommand = "talosctl apply-config --insecure -n <new-node-ip> -f controlplane.yaml"

/// The members a replacement is offered for: voting members with no healthy status (their
/// node did not answer, or its etcd reports errors). Learners are catching up, not failed.
public func replaceCandidates(_ etcd: EtcdOverview) -> [EtcdMember] {
    etcd.members.filter { member in
        !member.isLearner && !member.id.isEmpty
            && !etcd.statuses.contains { $0.memberId == member.id && $0.error == nil && $0.errors.isEmpty }
    }
}

/// The member's node address, from its client then peer URLs; "" when none parses.
public func etcdMemberAddress(_ member: EtcdMember) -> String {
    for raw in member.clientUrls + member.peerUrls {
        if let host = URLComponents(string: raw)?.host, !host.isEmpty {
            return host.trimmingCharacters(in: CharacterSet(charactersIn: "[]"))
        }
    }
    return ""
}

extension EtcdMember {
    /// A member known by id and hostname only (the replacement screen's removal sheet).
    public init(id: String, hostname: String) {
        self.id = id
        self.hostname = hostname
        peerUrls = []
        clientUrls = []
        isLearner = false
    }
}
