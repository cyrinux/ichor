import Foundation

/// One KEDA ScaledObject; current and desired come from the HPA it owns.
public struct ScaledObjectRow: Decodable, Equatable, Hashable, Identifiable, Sendable {
    public let namespace: String
    public let name: String
    public let target: AutoscaleTarget
    public let min: Int
    public let max: Int
    public let current: Int
    public let desired: Int
    public let triggers: [KedaTrigger]
    public let fallbackReplicas: Int
    public let paused: Bool
    /// Set when paused at a count.
    public let pausedReplicas: Int?
    public let ready: Bool
    public let active: Bool
    public let fallback: Bool
    public let message: String
    public let hpaName: String
    public let state: AutoscaleState

    public var id: String { "\(namespace)/\(name)" }

    public init(
        namespace: String = "", name: String = "", target: AutoscaleTarget = AutoscaleTarget(), min: Int = 0,
        max: Int = 0,
        current: Int = 0, desired: Int = 0, triggers: [KedaTrigger] = [], fallbackReplicas: Int = 0,
        paused: Bool = false,
        pausedReplicas: Int? = nil, ready: Bool = false, active: Bool = false, fallback: Bool = false,
        message: String = "",
        hpaName: String = "", state: AutoscaleState = .ok
    ) {
        self.namespace = namespace
        self.name = name
        self.target = target
        self.min = min
        self.max = max
        self.current = current
        self.desired = desired
        self.triggers = triggers
        self.fallbackReplicas = fallbackReplicas
        self.paused = paused
        self.pausedReplicas = pausedReplicas
        self.ready = ready
        self.active = active
        self.fallback = fallback
        self.message = message
        self.hpaName = hpaName
        self.state = state
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        target = try c.field(.target, AutoscaleTarget())
        min = try c.field(.min, 0)
        max = try c.field(.max, 0)
        current = try c.field(.current, 0)
        desired = try c.field(.desired, 0)
        triggers = try c.field(.triggers, [])
        fallbackReplicas = try c.field(.fallbackReplicas, 0)
        paused = try c.field(.paused, false)
        pausedReplicas = try c.field(.pausedReplicas, Int?.none)
        ready = try c.field(.ready, false)
        active = try c.field(.active, false)
        fallback = try c.field(.fallback, false)
        message = try c.field(.message, "")
        hpaName = try c.field(.hpaName, "")
        state = try c.wire(.state)
    }

    private enum CodingKeys: String, CodingKey {
        case namespace, name, target, min, max, current, desired, triggers, fallbackReplicas, paused, pausedReplicas,
            ready, active, fallback, message, hpaName, state
    }
}

public struct ScaledJobRow: Decodable, Equatable, Hashable, Identifiable, Sendable {
    public let namespace: String
    public let name: String
    public let triggers: [KedaTrigger]
    public let maxReplicaCount: Int
    public let paused: Bool
    public let ready: Bool
    public let message: String
    public let state: AutoscaleState

    public var id: String { "\(namespace)/\(name)" }

    public init(
        namespace: String = "", name: String = "", triggers: [KedaTrigger] = [], maxReplicaCount: Int = 0,
        paused: Bool = false,
        ready: Bool = false, message: String = "", state: AutoscaleState = .ok
    ) {
        self.namespace = namespace
        self.name = name
        self.triggers = triggers
        self.maxReplicaCount = maxReplicaCount
        self.paused = paused
        self.ready = ready
        self.message = message
        self.state = state
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        triggers = try c.field(.triggers, [])
        maxReplicaCount = try c.field(.maxReplicaCount, 0)
        paused = try c.field(.paused, false)
        ready = try c.field(.ready, false)
        message = try c.field(.message, "")
        state = try c.wire(.state)
    }

    private enum CodingKeys: String, CodingKey {
        case namespace, name, triggers, maxReplicaCount, paused, ready, message, state
    }
}

/// One trigger: its type and the one setting the user recognises. Never a secret.
public struct KedaTrigger: Decodable, Equatable, Hashable, Identifiable, Sendable {
    public let type: String
    public let summary: String

    public var label: String { [type, summary].filter { !$0.isEmpty }.joined(separator: ": ") }
    public var id: String { label }

    public init(type: String = "", summary: String = "") {
        self.type = type
        self.summary = summary
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        type = try c.field(.type, "")
        summary = try c.field(.summary, "")
    }

    private enum CodingKeys: String, CodingKey { case type, summary }
}

/// KubePauseScaledObject's special counts: hold at the current count, and resume.
public enum ScaledObjectPause {
    public static let hold = -1
    public static let resume = -2
}
