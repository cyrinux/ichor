import Foundation

/// One HorizontalPodAutoscaler. One KEDA created (managedBy keda) links its ScaledObject.
public struct HpaRow: Decodable, Equatable, Hashable, Identifiable, Sendable {
    public let namespace: String
    public let name: String
    public let target: AutoscaleTarget
    public let min: Int
    public let max: Int
    public let current: Int
    public let desired: Int
    public let metrics: [HpaMetric]
    public let conditions: HpaConditions
    public let atMax: Bool
    /// Unix ms, 0 when it never scaled.
    public let lastScaleAt: Int64
    public let managedBy: String
    public let scaledObject: String
    public let state: AutoscaleState

    public var id: String { "\(namespace)/\(name)" }

    public init(namespace: String = "", name: String = "", target: AutoscaleTarget = AutoscaleTarget(), min: Int = 0, max: Int = 0,
                current: Int = 0, desired: Int = 0, metrics: [HpaMetric] = [], conditions: HpaConditions = HpaConditions(),
                atMax: Bool = false, lastScaleAt: Int64 = 0, managedBy: String = "", scaledObject: String = "", state: AutoscaleState = .ok) {
        self.namespace = namespace
        self.name = name
        self.target = target
        self.min = min
        self.max = max
        self.current = current
        self.desired = desired
        self.metrics = metrics
        self.conditions = conditions
        self.atMax = atMax
        self.lastScaleAt = lastScaleAt
        self.managedBy = managedBy
        self.scaledObject = scaledObject
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
        metrics = try c.field(.metrics, [])
        conditions = try c.field(.conditions, HpaConditions())
        atMax = try c.field(.atMax, false)
        lastScaleAt = try c.field(.lastScaleAt, 0)
        managedBy = try c.field(.managedBy, "")
        scaledObject = try c.field(.scaledObject, "")
        state = try c.wire(.state)
    }

    private enum CodingKeys: String, CodingKey {
        case namespace, name, target, min, max, current, desired, metrics, conditions, atMax, lastScaleAt, managedBy, scaledObject, state
    }
}

/// One metric the HPA scales on: current against target, as the user reads them.
public struct HpaMetric: Decodable, Equatable, Hashable, Identifiable, Sendable {
    public let type: String
    public let name: String
    public let current: String
    public let target: String

    /// "cpu" for a Resource metric, else "External s0-prometheus".
    public var label: String { type == "Resource" ? name : [type, name].filter { !$0.isEmpty }.joined(separator: " ") }
    public var id: String { "\(type)/\(name)" }

    public init(type: String = "", name: String = "", current: String = "", target: String = "") {
        self.type = type
        self.name = name
        self.current = current
        self.target = target
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        type = try c.field(.type, "")
        name = try c.field(.name, "")
        current = try c.field(.current, "")
        target = try c.field(.target, "")
    }

    private enum CodingKeys: String, CodingKey { case type, name, current, target }
}

public struct HpaConditions: Decodable, Equatable, Hashable, Sendable {
    public let ableToScale: Bool
    public let scalingActive: Bool
    public let scalingLimited: Bool
    public let message: String

    public init(ableToScale: Bool = false, scalingActive: Bool = false, scalingLimited: Bool = false, message: String = "") {
        self.ableToScale = ableToScale
        self.scalingActive = scalingActive
        self.scalingLimited = scalingLimited
        self.message = message
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        ableToScale = try c.field(.ableToScale, false)
        scalingActive = try c.field(.scalingActive, false)
        scalingLimited = try c.field(.scalingLimited, false)
        message = try c.field(.message, "")
    }

    private enum CodingKeys: String, CodingKey { case ableToScale, scalingActive, scalingLimited, message }
}

/// "2 of 1–10": where the replica count sits between the bounds.
public func hpaBoundsText(_ current: Int, _ min: Int, _ max: Int) -> String { "\(current) of \(min)–\(max)" }

/// The bounds the core accepts for an HPA: min at least 1, max not below min nor above `limit`.
public enum HpaBounds {
    public static func isValid(_ min: Int, _ max: Int, limit: Int = kubeMaxScaleReplicas) -> Bool {
        min >= 1 && max >= min && max <= limit
    }
}

extension HpaRow {
    /// KEDA made this HPA for a ScaledObject: its bounds are the ScaledObject's, KEDA puts them back.
    public var isKEDAOwned: Bool { managedBy == "keda" }
}
