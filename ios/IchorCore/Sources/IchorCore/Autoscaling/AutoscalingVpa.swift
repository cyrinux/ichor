import Foundation

/// One VerticalPodAutoscaler with its recommendation per container.
public struct VpaRow: Decodable, Equatable, Hashable, Identifiable, Sendable {
    public let namespace: String
    public let name: String
    public let target: AutoscaleTarget
    /// Off, Initial, Recreate, Auto or InPlaceOrRecreate.
    public let updateMode: String
    public let containers: [VpaContainer]
    public let recommendationProvided: Bool
    public let message: String
    public let state: AutoscaleState

    public var id: String { "\(namespace)/\(name)" }

    public init(
        namespace: String = "", name: String = "", target: AutoscaleTarget = AutoscaleTarget(), updateMode: String = "",
        containers: [VpaContainer] = [], recommendationProvided: Bool = false, message: String = "",
        state: AutoscaleState = .ok
    ) {
        self.namespace = namespace
        self.name = name
        self.target = target
        self.updateMode = updateMode
        self.containers = containers
        self.recommendationProvided = recommendationProvided
        self.message = message
        self.state = state
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        target = try c.field(.target, AutoscaleTarget())
        updateMode = try c.field(.updateMode, "")
        containers = try c.field(.containers, [])
        recommendationProvided = try c.field(.recommendationProvided, false)
        message = try c.field(.message, "")
        state = try c.wire(.state)
    }

    private enum CodingKeys: String, CodingKey {
        case namespace, name, target, updateMode, containers, recommendationProvided, message, state
    }
}

/// The recommender's answer for one container next to its live requests ("" when unread).
public struct VpaContainer: Decodable, Equatable, Hashable, Identifiable, Sendable {
    public let name: String
    public let cpuTarget: String
    public let memoryTarget: String
    public let cpuLower: String
    public let cpuUpper: String
    public let memoryLower: String
    public let memoryUpper: String
    public let cpuRequest: String
    public let memoryRequest: String

    public var id: String { name }

    public init(
        name: String = "", cpuTarget: String = "", memoryTarget: String = "", cpuLower: String = "",
        cpuUpper: String = "",
        memoryLower: String = "", memoryUpper: String = "", cpuRequest: String = "", memoryRequest: String = ""
    ) {
        self.name = name
        self.cpuTarget = cpuTarget
        self.memoryTarget = memoryTarget
        self.cpuLower = cpuLower
        self.cpuUpper = cpuUpper
        self.memoryLower = memoryLower
        self.memoryUpper = memoryUpper
        self.cpuRequest = cpuRequest
        self.memoryRequest = memoryRequest
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        cpuTarget = try c.field(.cpuTarget, "")
        memoryTarget = try c.field(.memoryTarget, "")
        cpuLower = try c.field(.cpuLower, "")
        cpuUpper = try c.field(.cpuUpper, "")
        memoryLower = try c.field(.memoryLower, "")
        memoryUpper = try c.field(.memoryUpper, "")
        cpuRequest = try c.field(.cpuRequest, "")
        memoryRequest = try c.field(.memoryRequest, "")
    }

    private enum CodingKeys: String, CodingKey {
        case name, cpuTarget, memoryTarget, cpuLower, cpuUpper, memoryLower, memoryUpper, cpuRequest, memoryRequest
    }
}

/// "100m → 250m": the live request against what the recommender wants (the CAST AI style).
public func vpaChangeText(_ request: String, _ target: String) -> String {
    if request.isEmpty { return target }
    if target.isEmpty { return request }
    if request == target { return target }
    return "\(request) → \(target)"
}
