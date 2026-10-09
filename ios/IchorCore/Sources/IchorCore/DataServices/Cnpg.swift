import Foundation

// Data services: CloudNativePG clusters (see DataServices.swift).

public struct CnpgStatus: Decodable, Equatable, Sendable {
    public let version: String
    public let error: String
    public let clusters: [CnpgCluster]

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        version = try c.field(.version, "")
        error = try c.field(.error, "")
        clusters = try c.field(.clusters, [])
    }

    /// Postgres instances waiting unscheduled (no node): often their volume is pinned to a node that is gone.
    public var pendingInstances: Int {
        clusters.reduce(0) { $0 + $1.instancePods.filter { $0.node.isEmpty && $0.phase == "Pending" }.count }
    }

    private enum CodingKeys: String, CodingKey { case version, error, clusters }
}

public struct CnpgCluster: Decodable, Equatable, Identifiable, Sendable {
    public let namespace: String
    public let name: String
    public let phase: String
    public let phaseReason: String
    public let health: ServiceHealth
    public let hibernated: Bool
    /// Known reasons only; values from newer cores are dropped.
    public let reasons: [CnpgReason]
    public let instances: Int
    public let readyInstances: Int
    public let currentPrimary: String
    public let targetPrimary: String
    public let instancePods: [CnpgPod]
    /// ok, failing, off or unknown.
    public let archiving: String
    /// ok, failed, stale or none.
    public let lastBackup: String
    /// plugin, in-tree or none.
    public let backupMethod: String
    public let objectStore: String
    public let scheduled: Bool
    public let lastSuccessAt: Int64
    public let lastFailureAt: Int64
    public let recoverableAt: Int64

    public var id: String { label }
    public var label: String { "\(namespace)/\(name)" }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.decode(String.self, forKey: .namespace)
        name = try c.decode(String.self, forKey: .name)
        phase = try c.field(.phase, "")
        phaseReason = try c.field(.phaseReason, "")
        health = try c.wire(.health)
        hibernated = try c.field(.hibernated, false)
        reasons = try c.wireList(.reasons)
        instances = try c.field(.instances, 0)
        readyInstances = try c.field(.readyInstances, 0)
        currentPrimary = try c.field(.currentPrimary, "")
        targetPrimary = try c.field(.targetPrimary, "")
        instancePods = try c.field(.instancePods, [])
        archiving = try c.field(.archiving, "")
        lastBackup = try c.field(.lastBackup, "")
        backupMethod = try c.field(.backupMethod, "")
        objectStore = try c.field(.objectStore, "")
        scheduled = try c.field(.scheduled, false)
        lastSuccessAt = try c.field(.lastSuccessfulBackupAt, 0)
        lastFailureAt = try c.field(.lastFailedBackupAt, 0)
        recoverableAt = try c.field(.firstRecoverabilityAt, 0)
    }

    private enum CodingKeys: String, CodingKey {
        case namespace, name, phase, phaseReason, health, hibernated, reasons, instances, readyInstances
        case currentPrimary, targetPrimary, instancePods, archiving, lastBackup, backupMethod, objectStore, scheduled
        case lastSuccessfulBackupAt, lastFailedBackupAt, firstRecoverabilityAt
    }
}

public typealias CnpgPod = ServicePod

/// Why a Postgres cluster is not ok, as the Go core names it.
public enum CnpgReason: String, Sendable {
    case noInstance, failover, instances, switchover, notReady, archiving, backupFailed, backupStale
}

/// Clusters matching query by namespace/name, only those needing attention when problemsOnly.
public func filterClusters(_ clusters: [CnpgCluster], problemsOnly: Bool, query: String) -> [CnpgCluster] {
    let q = query.trimmingCharacters(in: .whitespaces)
    return clusters.filter { c in
        (!problemsOnly || c.health.needsAttention) && (q.isEmpty || c.label.localizedCaseInsensitiveContains(q))
    }
}
