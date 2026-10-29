import Foundation
public struct LonghornSalvageReplica: Decodable, Identifiable, Sendable {
    public let name: String
    public let node: String
    public let healthyAt: Int64
    public let failedAt: Int64
    public let eligible: Bool
    /// Why Longhorn would refuse to salvage from this replica (node-missing, node-not-ready,
    /// disk-unavailable, deleting); empty when eligible.
    public let blocked: String
    public var id: String { name }
}
public struct LonghornSalvagePreview: Decodable, Sendable {
    public let namespace: String
    public let name: String
    public let state: String
    public let robustness: String
    public let canSalvage: Bool
    public let error: String
    public let replicas: [LonghornSalvageReplica]
    public func accepts(_ selection: Set<String>) -> Bool {
        canSalvage && error.isEmpty && !selection.isEmpty
            && selection.allSatisfy { name in replicas.contains { $0.name == name && $0.eligible } }
    }
}
