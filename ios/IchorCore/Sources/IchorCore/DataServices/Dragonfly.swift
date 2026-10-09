import Foundation

// Data services: Dragonfly instances (see DataServices.swift).

public struct DragonflyStatus: Decodable, Equatable, Sendable {
    public let version: String
    public let error: String
    public let instances: [DragonflyInstance]

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        version = try c.field(.version, "")
        error = try c.field(.error, "")
        instances = try c.field(.instances, [])
    }

    private enum CodingKeys: String, CodingKey { case version, error, instances }
}

public struct DragonflyInstance: Decodable, Equatable, Identifiable, Sendable {
    public let namespace: String
    public let name: String
    /// The operator's own word: Ready, or a step such as a rolling update.
    public let phase: String
    public let health: ServiceHealth
    /// Known reasons only; values from newer cores are dropped.
    public let reasons: [DragonflyReason]
    public let replicas: Int
    public let readyPods: Int
    /// Pod with role=master, "" when none.
    public let master: String
    /// The master first.
    public let pods: [DragonflyPod]

    public var id: String { label }
    public var label: String { "\(namespace)/\(name)" }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        phase = try c.field(.phase, "")
        health = try c.wire(.health)
        reasons = try c.wireList(.reasons)
        replicas = try c.field(.replicas, 0)
        readyPods = try c.field(.readyPods, 0)
        master = try c.field(.master, "")
        pods = try c.field(.pods, [])
    }

    private enum CodingKeys: String, CodingKey { case namespace, name, phase, health, reasons, replicas, readyPods, master, pods }
}

public typealias DragonflyPod = ServicePod

/// Why a Dragonfly instance is not ok, as the Go core names it.
public enum DragonflyReason: String, Sendable {
    case noReady, noMaster, masters, pods, notReady
}
