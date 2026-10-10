import Foundation

// Mirrors go/ichorgo/etcdfix.go (StartEtcdNospaceFix).

extension EtcdOverview {
    /// The NOSPACE alarm is active: the one-tap fix is offered.
    public var hasNospace: Bool { alarms.contains { $0.alarm == "NOSPACE" } }
}

/// A member during the fix's defragmentation.
public struct EtcdFixMember: Decodable, Equatable, Identifiable, Sendable {
    public enum State: String, Sendable {
        case pending, running, done, failed
    }

    public let node: String
    public let hostname: String
    /// Go's state; an unknown one reads as pending.
    public let state: String
    /// What its database shrank by, known once etcd was read again.
    public let reclaimedBytes: Int64

    public var id: String { node }
    public var memberState: State { State(rawValue: state) ?? .pending }

    public init(node: String, hostname: String = "", state: String = State.pending.rawValue, reclaimedBytes: Int64 = 0) {
        self.node = node
        self.hostname = hostname
        self.state = state
        self.reclaimedBytes = reclaimedBytes
    }

    private enum CodingKeys: String, CodingKey { case node, hostname, state, reclaimedBytes }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        node = try c.field(.node, "")
        hostname = try c.field(.hostname, "")
        state = try c.field(.state, State.pending.rawValue)
        reclaimedBytes = try c.field(.reclaimedBytes, 0)
    }
}

/// One OnProgress event of the fix: `at` is unix ms, `step` 1-based.
public struct EtcdFixProgress: Decodable, Equatable, Sendable {
    public let phase: String
    public let message: String
    public let at: Int64
    public let step: Int
    public let steps: Int
    public let members: [EtcdFixMember]

    public init(phase: String, message: String = "", at: Int64 = 0, step: Int = 0, steps: Int = 0, members: [EtcdFixMember] = []) {
        self.phase = phase
        self.message = message
        self.at = at
        self.step = step
        self.steps = steps
        self.members = members
    }

    private enum CodingKeys: String, CodingKey { case phase, message, at, step, steps, members }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        phase = try c.field(.phase, "")
        message = try c.field(.message, "")
        at = try c.field(.at, 0)
        step = try c.field(.step, 0)
        steps = try c.field(.steps, 0)
        members = try c.field(.members, [])
    }
}

/// The fix's steps, in order.
public enum EtcdFixPhase: String, CaseIterable, Comparable, Sendable {
    case snapshot, defrag, disarm, recheck

    public static func < (a: Self, b: Self) -> Bool {
        allCases.firstIndex(of: a)! < allCases.firstIndex(of: b)!
    }
}

/// Timeline of the fix's steps from the events so far, like maintenanceTimeline.
public func etcdFixTimeline(_ events: [EtcdFixProgress], finished: Bool = false, failure: String? = nil) -> [TimelineStep<EtcdFixPhase>] {
    foldTimeline(events, phases: EtcdFixPhase.allCases, finished: finished, failure: failure,
                 phase: { EtcdFixPhase(rawValue: $0.phase) }, at: \.at, message: \.message)
}
