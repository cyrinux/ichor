import Foundation

// Mirrors go/ichorgo/kube_mariadb.go (the wire format is documented in plans/data-services/README.md).

public struct MariaDbStatus: Decodable, Equatable, Sendable {
    public let version: String
    public let error: String
    public let clusters: [MariaDbCluster]

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        version = try c.field(.version, "")
        error = try c.field(.error, "")
        clusters = try c.field(.clusters, [])
    }

    var summary: ServiceSummary {
        if !error.isEmpty && clusters.isEmpty { return ServiceSummary(total: 0, attention: 0, health: .unknown, error: error) }
        let healths = clusters.map(\.health)
        return ServiceSummary(total: clusters.count, attention: healths.filter(\.needsAttention).count, health: .worst(healths), error: error)
    }

    private enum CodingKeys: String, CodingKey { case version, error, clusters }
}

public struct MariaDbCluster: Decodable, Equatable, Identifiable, Sendable {
    public let namespace: String
    public let name: String
    /// standalone, replication or galera.
    public let topology: String
    public let health: ServiceHealth
    /// Known reasons only; values from newer cores are dropped.
    public let reasons: [MariaDbReason]
    public let suspended: Bool
    /// The operator's Ready condition message when it is not True.
    public let message: String
    public let replicas: Int
    public let readyPods: Int
    /// The operator's current primary, "" when none.
    public let primary: String
    /// The primary first.
    public let pods: [MariaDbPod]
    /// Last successful and failed backup (logical or physical), unix ms, 0 when none.
    public let lastBackupAt: Int64
    public let lastBackupFailedAt: Int64
    /// The most frequent active backup cron, "" when none.
    public let backupSchedule: String

    public var id: String { label }
    public var label: String { "\(namespace)/\(name)" }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        topology = try c.field(.topology, "")
        health = try c.wire(.health)
        reasons = try c.wireList(.reasons)
        suspended = try c.field(.suspended, false)
        message = try c.field(.message, "")
        replicas = try c.field(.replicas, 0)
        readyPods = try c.field(.readyPods, 0)
        primary = try c.field(.primary, "")
        pods = try c.field(.pods, [])
        lastBackupAt = try c.field(.lastBackupAt, 0)
        lastBackupFailedAt = try c.field(.lastBackupFailedAt, 0)
        backupSchedule = try c.field(.backupSchedule, "")
    }

    private enum CodingKeys: String, CodingKey {
        case namespace, name, topology, health, reasons, suspended, message, replicas, readyPods, primary, pods
        case lastBackupAt, lastBackupFailedAt, backupSchedule
    }
}

public typealias MariaDbPod = ServicePod

/// Why a MariaDB cluster is not ok, as the Go core names it.
public enum MariaDbReason: String, Sendable {
    case noReady, noPrimary, pods, galeraRecovery, backupFailed, backupStale, notReady
}
