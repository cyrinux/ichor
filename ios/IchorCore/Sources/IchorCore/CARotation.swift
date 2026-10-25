import Foundation

// Mirrors go/ichorgo/rotateca_plan.go and rotateca_run.go (Android: model/CARotation.kt).

/// A CA certificate: its subject, validity (unix seconds) and the start of its SHA-256.
public struct CARotationCA: Decodable, Equatable, Sendable {
    public let subject: String
    public let notBefore: Int64
    public let notAfter: Int64
    public let fingerprint: String

    private enum CodingKeys: String, CodingKey { case subject, notBefore, notAfter, fingerprint }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        subject = try c.field(.subject, "")
        notBefore = try c.field(.notBefore, 0)
        notAfter = try c.field(.notAfter, 0)
        fingerprint = try c.field(.fingerprint, "")
    }
}

/// A node the rotation patches; `problem` says why it stands in the way ("" when it does not).
public struct CARotationNode: Decodable, Equatable, Sendable, Identifiable {
    public let node: String
    public let hostname: String
    public let role: String
    public let version: String
    public let reachable: Bool
    public let acceptedCAs: Int
    public let problem: String

    public var id: String { node }
    public var label: String { hostname.isEmpty ? node : hostname }
    public var health: ServiceHealth { problem.isEmpty ? .ok : .critical }

    private enum CodingKeys: String, CodingKey { case node, hostname, role, version, reachable, acceptedCAs, problem }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        node = try c.field(.node, "")
        hostname = try c.field(.hostname, "")
        role = try c.field(.role, "")
        version = try c.field(.version, "")
        reachable = try c.field(.reachable, false)
        acceptedCAs = try c.field(.acceptedCAs, 0)
        problem = try c.field(.problem, "")
    }
}

/// What TalosCARotationPlan says: the CA, the nodes, and what stands in the way.
public struct CARotationPlan: Decodable, Equatable, Sendable {
    public let context: String
    public let endpoints: [String]
    public let ca: CARotationCA?
    public let nodes: [CARotationNode]
    /// The nodes are every member cluster discovery lists, not only the talosconfig's.
    public let discovered: Bool
    public let blockers: [String]
    public let warnings: [String]
    public let ready: Bool

    private enum CodingKeys: String, CodingKey { case context, endpoints, ca, nodes, discovered, blockers, warnings, ready }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        context = try c.field(.context, "")
        endpoints = try c.field(.endpoints, [])
        ca = try c.decodeIfPresent(CARotationCA.self, forKey: .ca)
        nodes = try c.field(.nodes, [])
        discovered = try c.field(.discovered, false)
        blockers = try c.field(.blockers, [])
        warnings = try c.field(.warnings, [])
        ready = try c.field(.ready, false)
    }
}

public struct CARotationProgressNode: Decodable, Equatable, Sendable, Identifiable {
    public let node: String
    public let hostname: String
    public let role: String
    public let state: String

    public var id: String { node }
    public var label: String { hostname.isEmpty ? node : hostname }

    /// How the node's state in the current phase reads as health.
    public var health: ServiceHealth {
        switch state {
        case "done": .ok
        case "failed": .critical
        case "patching", "verifying": .warning
        default: .idle
        }
    }

    private enum CodingKeys: String, CodingKey { case node, hostname, role, state }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        node = try c.field(.node, "")
        hostname = try c.field(.hostname, "")
        role = try c.field(.role, "")
        state = try c.field(.state, "")
    }
}

/// One event of StartTalosCARotation.
public struct CARotationProgress: Decodable, Equatable, Sendable {
    public let phase: String
    public let step: Int
    public let total: Int
    public let node: String
    public let hostname: String
    public let message: String
    public let at: Int64
    public let nodes: [CARotationProgressNode]

    public init(phase: String, step: Int = 0, total: Int = 0, message: String = "", at: Int64 = 0) {
        self.phase = phase
        self.step = step
        self.total = total
        node = ""
        hostname = ""
        self.message = message
        self.at = at
        nodes = []
    }

    private enum CodingKeys: String, CodingKey { case phase, step, total, node, hostname, message, at, nodes }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        phase = try c.field(.phase, "")
        step = try c.field(.step, 0)
        total = try c.field(.total, 0)
        node = try c.field(.node, "")
        hostname = try c.field(.hostname, "")
        message = try c.field(.message, "")
        at = try c.field(.at, 0)
        nodes = try c.field(.nodes, [])
    }
}

/// The phases of a rotation, in order; `done` ends it and is no step of the timeline.
public enum CARotationPhase: String, CaseIterable, Comparable, Sendable {
    case check, accept, handover, swap, drop, done

    public static let steps: [CARotationPhase] = allCases.filter { $0 != .done }

    public static func < (a: Self, b: Self) -> Bool {
        allCases.firstIndex(of: a)! < allCases.firstIndex(of: b)!
    }
}

public typealias CARotationStep = TimelineStep<CARotationPhase>

/// The rotation's timeline from the events so far, like joinTimeline: phases only move forward,
/// `finished` (or reaching done) marks them all done, `failure` marks the furthest one failed.
public func caRotationTimeline(_ events: [CARotationProgress], finished: Bool = false, failure: String? = nil) -> [CARotationStep] {
    foldTimeline(events, phases: CARotationPhase.allCases, finished: finished, failure: failure, terminal: .done,
                 phase: { CARotationPhase(rawValue: $0.phase) }, at: \.at, message: \.message)
        .filter { $0.phase != .done }
}
