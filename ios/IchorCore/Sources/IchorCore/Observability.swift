import Foundation

public struct NetworkCounters: Codable, Equatable, Sendable {
    public let name: String
    public let rx, tx, errors, drops: UInt64
}
public struct DiskCounters: Codable, Equatable, Sendable {
    public let name: String
    public let read, write, operations, timeMs, busyMs: UInt64
}
public struct DeviceRate: Decodable, Equatable, Identifiable, Sendable {
    public let name: String
    public let read, write, errors, drops, busy, latency: Double
    public var id: String { name }
}
public struct Bottlenecks: Decodable, Equatable, Sendable {
    public let wait, steal: Double
    public let network, disks: [DeviceRate]
    public let errors: [String: String]
}
public struct DriftNode: Decodable, Equatable, Identifiable, Sendable {
    public let node, hostname, role: String
    public let values, errors: [String: String]
    public var id: String { node }
}
public struct DriftSnapshot: Decodable, Equatable, Sendable {
    public let scope: String
    public let at: Int64
    public let nodes: [DriftNode]
}
public struct DriftChange: Decodable, Equatable, Identifiable, Sendable {
    public let node, reference, key, before, after: String
    public var id: String { "\(node)/\(key)" }
}
public struct IncidentEntry: Decodable, Equatable, Identifiable, Sendable {
    public let id: String
    public let at: Int64
    public let node, kind, subject, detail, severity: String
}
public struct IncidentDocument: Decodable, Equatable, Sendable {
    public let scope: String
    public let startedAt, updatedAt: Int64
    public let dropped: Int
    public let entries: [IncidentEntry]
}
