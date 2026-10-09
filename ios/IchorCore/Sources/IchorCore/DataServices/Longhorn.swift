import Foundation

// Data services: Longhorn volumes, nodes and backup targets (see DataServices.swift).

public struct LonghornStatus: Decodable, Equatable, Sendable {
    public let version: String
    /// Installed but could not be read.
    public let error: String
    public let volumes: [LonghornVolume]
    public let nodes: [LonghornNode]
    public let backupTargets: [LonghornBackupTarget]

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        version = try c.field(.version, "")
        error = try c.field(.error, "")
        volumes = try c.field(.volumes, [])
        nodes = try c.field(.nodes, [])
        backupTargets = try c.field(.backupTargets, [])
    }

    private enum CodingKeys: String, CodingKey { case version, error, volumes, nodes, backupTargets }
}

public struct LonghornVolume: Decodable, Equatable, Identifiable, Sendable {
    public let name: String
    public let namespace: String
    /// "" when no claim is bound.
    public let pvcNamespace: String
    public let pvcName: String
    /// creating, attached, detached, attaching, detaching or deleting.
    public let state: String
    /// healthy, degraded, faulted or unknown.
    public let robustness: String
    public let health: ServiceHealth
    public let replicasDesired: Int
    public let replicasHealthy: Int
    public let rebuilding: Int
    /// Nodes holding a replica, failed ones too.
    public let replicaNodes: [String]
    /// Where it is attached.
    public let node: String
    public let size: Int64
    public let actualSize: Int64
    /// Unix ms, 0 when never.
    public let lastBackupAt: Int64
    /// Progress of what the engine runs, 0-100: the slowest replica rebuild (with rebuilding > 0),
    /// a backup and a restore in flight.
    public let rebuildProgress: Int
    public let backingUp: Bool
    public let backupProgress: Int
    public let restoring: Bool
    public let restoreProgress: Int
    /// Why a replica cannot be placed, "" when it can.
    public let scheduleError: String
    /// Close to Longhorn's snapshot limit.
    public let tooManySnapshots: Bool

    public var id: String { name }

    /// The claim it backs ("namespace/name"), or the volume's own name when unbound.
    public var label: String { pvcName.isEmpty ? name : "\(pvcNamespace)/\(pvcName)" }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.decode(String.self, forKey: .name)
        namespace = try c.field(.namespace, "")
        pvcNamespace = try c.field(.pvcNamespace, "")
        pvcName = try c.field(.pvcName, "")
        state = try c.field(.state, "")
        robustness = try c.field(.robustness, "")
        health = try c.wire(.health)
        replicasDesired = try c.field(.replicasDesired, 0)
        replicasHealthy = try c.field(.replicasHealthy, 0)
        rebuilding = try c.field(.rebuilding, 0)
        replicaNodes = try c.field(.replicaNodes, [])
        node = try c.field(.node, "")
        size = try c.field(.size, 0)
        actualSize = try c.field(.actualSize, 0)
        lastBackupAt = try c.field(.lastBackupAt, 0)
        rebuildProgress = try c.field(.rebuildProgress, 0)
        backingUp = try c.field(.backingUp, false)
        backupProgress = try c.field(.backupProgress, 0)
        restoring = try c.field(.restoring, false)
        restoreProgress = try c.field(.restoreProgress, 0)
        scheduleError = try c.field(.scheduleError, "")
        tooManySnapshots = try c.field(.tooManySnapshots, false)
    }

    private enum CodingKeys: String, CodingKey {
        case name, namespace, pvcNamespace, pvcName, state, robustness, health, replicasDesired, replicasHealthy
        case rebuilding, replicaNodes, node, size, actualSize, lastBackupAt
        case rebuildProgress, backingUp, backupProgress, restoring, restoreProgress, scheduleError, tooManySnapshots
    }
}

public struct LonghornNode: Decodable, Equatable, Identifiable, Sendable {
    public let name: String
    /// Longhorn's own, where the node object lives.
    public let namespace: String
    public let ready: Bool
    public let schedulable: Bool
    /// What the user asked: new replicas on the node, its replicas moved away.
    public let allowScheduling: Bool
    public let evictionRequested: Bool
    /// Replicas it holds, failed ones too.
    public let replicas: Int
    public let disks: [LonghornDisk]

    public var id: String { name }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.decode(String.self, forKey: .name)
        namespace = try c.field(.namespace, "")
        ready = try c.field(.ready, false)
        schedulable = try c.field(.schedulable, false)
        // An older core did not send it: scheduling was on as far as the app knew.
        allowScheduling = try c.field(.allowScheduling, true)
        evictionRequested = try c.field(.evictionRequested, false)
        replicas = try c.field(.replicas, 0)
        disks = try c.field(.disks, [])
    }

    private enum CodingKeys: String, CodingKey {
        case name, namespace, ready, schedulable, allowScheduling, evictionRequested, replicas, disks
    }
}

public struct LonghornDisk: Decodable, Equatable, Sendable {
    public let path: String
    public let schedulable: Bool
    public let available: Int64
    public let maximum: Int64
    public let scheduled: Int64

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        path = try c.field(.path, "")
        schedulable = try c.field(.schedulable, false)
        available = try c.field(.available, 0)
        maximum = try c.field(.maximum, 0)
        scheduled = try c.field(.scheduled, 0)
    }

    private enum CodingKeys: String, CodingKey { case path, schedulable, available, maximum, scheduled }
}

public struct LonghornBackupTarget: Decodable, Equatable, Identifiable, Sendable {
    public let name: String
    public let url: String
    public let available: Bool
    public let message: String

    public var id: String { "\(name)|\(url)" }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        url = try c.field(.url, "")
        available = try c.field(.available, false)
        message = try c.field(.message, "")
    }

    private enum CodingKeys: String, CodingKey { case name, url, available, message }
}

public enum VolumeFilter: Hashable, Sendable, CaseIterable {
    case problems, all, detached
}

/// Volumes of filter whose claim or name contains query (case-insensitive), in the Go core's order.
public func filterVolumes(_ volumes: [LonghornVolume], filter: VolumeFilter, query: String) -> [LonghornVolume] {
    let q = query.trimmingCharacters(in: .whitespaces)
    return volumes.filter { v in
        let kept: Bool
        switch filter {
        case .all: kept = true
        case .problems: kept = v.health.needsAttention
        case .detached: kept = v.state == "detached"
        }
        return kept && (q.isEmpty || v.label.localizedCaseInsensitiveContains(q) || v.name.localizedCaseInsensitiveContains(q))
    }
}
