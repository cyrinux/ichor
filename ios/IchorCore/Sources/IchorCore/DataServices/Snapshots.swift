import Foundation

public struct VolumeSnapshots: Decodable, Equatable, Sendable {
    public let installed: Bool
    public let version, error, contentsError, classesError, claimsError: String
    public let snapshots: [VolumeSnapshot]
    public let contents: [SnapshotContent]
    public let classes: [SnapshotClass]
    public let claims: [SnapshotClaim]
    public var summary: ServiceSummary {
        let attention = claims.filter { $0.watched && $0.stale }.count + snapshots.filter { !$0.error.isEmpty }.count
        return ServiceSummary(total: snapshots.count, attention: attention, health: attention > 0 ? .warning : .ok,
                              error: [error, contentsError, classesError, claimsError].filter { !$0.isEmpty }.joined(separator: "; "))
    }
    public func canCreate(_ claim: StorageClaim) -> Bool {
        installed && classesError.isEmpty && claim.phase == "Bound" && !claim.terminating && !claim.provisioner.isEmpty &&
        classes.filter { $0.default && $0.driver == claim.provisioner }.count == 1
    }
}
public struct VolumeSnapshot: Decodable, Equatable, Identifiable, Sendable {
    public let namespace, name, pvc, className, content, error: String
    public let ready: Bool
    public let createdAt, ageSeconds: Int64
    public var id: String { namespace + "/" + name }
    private enum CodingKeys: String, CodingKey { case namespace, name, pvc, className = "class", content, error, ready, createdAt, ageSeconds }
}
public struct SnapshotContent: Decodable, Equatable, Identifiable, Sendable {
    public let name, namespace, snapshot, className, driver, error: String
    public let ready: Bool
    public let createdAt: Int64
    public var id: String { name }
    private enum CodingKeys: String, CodingKey { case name, namespace, snapshot, className = "class", driver, error, ready, createdAt }
}
public struct SnapshotClass: Decodable, Equatable, Identifiable, Sendable {
    public let name, driver, deletionPolicy: String
    public let `default`: Bool
    public var id: String { name }
}
public struct SnapshotClaim: Decodable, Equatable, Identifiable, Sendable {
    public let namespace, name: String
    public let lastGoodAt, ageSeconds: Int64
    public let watched, stale: Bool
    public var id: String { namespace + "/" + name }
}
public struct BackupJobStatus: Decodable, Equatable, Sendable {
    public let error, jobsError: String
    public let jobs: [BackupCronJob]
    public var summary: ServiceSummary {
        ServiceSummary(total: jobs.count, attention: jobs.filter { ServiceHealth(wire: $0.health).needsAttention }.count,
                       health: .worst(jobs.map { ServiceHealth(wire: $0.health) }),
                       error: [error, jobsError].filter { !$0.isEmpty }.joined(separator: "; "))
    }
}
public struct BackupCronJob: Decodable, Equatable, Identifiable, Sendable {
    public let namespace, name, schedule, health: String
    public let suspended: Bool
    public let lastSuccess, ageSeconds: Int64
    public let failureStreak: Int
    public var id: String { namespace + "/" + name }
}
