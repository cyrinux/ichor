import Foundation

// Data services: Longhorn volumes, nodes and backup targets (see DataServices.swift).

public struct LonghornStatus: Decodable, Equatable, Sendable {
    public let version: String
    /// Installed but could not be read.
    public let error: String
    public let volumes: [LonghornVolume]
    public let nodes: [LonghornNode]
    public let backupTargets: [LonghornBackupTarget]
    /// Longhorn's `default-engine-image` setting, "" when unread.
    public let defaultEngineImage: String
    /// The engine images, the default first.
    public let engineImages: [LonghornEngineImageStatus]

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        version = try c.field(.version, "")
        error = try c.field(.error, "")
        volumes = try c.field(.volumes, [])
        nodes = try c.field(.nodes, [])
        backupTargets = try c.field(.backupTargets, [])
        defaultEngineImage = try c.field(.defaultEngineImage, "")
        engineImages = try c.field(.engineImages, [])
    }

    /// The default engine's version ("v1.13.0"), its image tag when the version is unknown.
    public var defaultEngineVersion: String {
        engineImages.first { $0.image == defaultEngineImage }?.version.nonEmpty ?? longhornImageTag(defaultEngineImage)
    }

    /// Volumes whose engine lags the default image.
    public var outdatedEngineVolumes: Int {
        // Only when the cluster really runs more than one engine (as on Android).
        guard engineImages.filter({ $0.refCount > 0 }).count > 1 else { return 0 }
        return volumes.filter(\.engineOutdated).count
    }

    private enum CodingKeys: String, CodingKey { case version, error, volumes, nodes, backupTargets, defaultEngineImage, engineImages }
}

/// A Longhorn engine image (engineimages.longhorn.io) as the status lists it.
public struct LonghornEngineImageStatus: Decodable, Equatable, Identifiable, Sendable {
    public let image: String
    public let version: String
    /// deploying or deployed.
    public let state: String
    /// Volumes running it.
    public let refCount: Int
    public let incompatible: Bool
    public let deployedNodes: Int
    public let nodes: Int

    public var id: String { image }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        image = try c.field(.image, "")
        version = try c.field(.version, "")
        state = try c.field(.state, "")
        refCount = try c.field(.refCount, 0)
        incompatible = try c.field(.incompatible, false)
        deployedNodes = try c.field(.deployedNodes, 0)
        nodes = try c.field(.nodes, 0)
    }

    private enum CodingKeys: String, CodingKey { case image, version, state, refCount, incompatible, deployedNodes, nodes }
}

/// The tag of an image reference ("v1.12.0" of "longhornio/longhorn-engine:v1.12.0"), "" without one.
public func longhornImageTag(_ image: String) -> String {
    let last = image.split(separator: "/").last.map(String.init) ?? image
    guard let colon = last.lastIndex(of: ":") else { return "" }
    return String(last[last.index(after: colon)...])
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
    /// Longhorn's own snapshots still in use, newest first.
    public let snapshots: [LonghornSnapshot]
    /// Deleted snapshots Longhorn has yet to purge.
    public let snapshotsRemoving: Int
    /// Newest backups first, at most 20: backupCount has them all.
    public let backups: [LonghornBackup]
    public let backupCount: Int
    /// The last backup is more than a week old.
    public let backupStale: Bool
    /// The VolumeSnapshotContents were read: a snapshot without volumeSnapshot keeps none.
    public let volumeSnapshotsKnown: Bool
    /// The engine image asked for (spec.image) and the one running (status.currentImage).
    public let engineImage: String
    public let engineCurrentImage: String
    /// The two differ: Longhorn is replacing the engine.
    public let engineUpgrading: Bool
    /// The running engine is not the default one, which is deployed.
    public let engineOutdated: Bool
    /// The running engine's version, "" when unknown.
    public let engineVersion: String

    public var id: String { name }

    public var attached: Bool { state == "attached" }

    /// Its engine can be upgraded now: outdated, not already upgrading, attached (live) or detached (offline).
    public var engineUpgradable: Bool {
        engineOutdated && !engineUpgrading && (state == "attached" || state == "detached")
    }

    /// The running engine's version, else its image tag.
    public var engineLabel: String { engineVersion.nonEmpty ?? longhornImageTag(engineCurrentImage) }

    /// The newest backup failed.
    public var latestBackupFailed: Bool { backups.first?.failed ?? false }

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
        snapshots = try c.field(.snapshots, [])
        snapshotsRemoving = try c.field(.snapshotsRemoving, 0)
        backups = try c.field(.backups, [])
        backupCount = try c.field(.backupCount, 0)
        backupStale = try c.field(.backupStale, false)
        volumeSnapshotsKnown = try c.field(.volumeSnapshotsKnown, false)
        engineImage = try c.field(.engineImage, "")
        engineCurrentImage = try c.field(.engineCurrentImage, "")
        engineUpgrading = try c.field(.engineUpgrading, false)
        engineOutdated = try c.field(.engineOutdated, false)
        engineVersion = try c.field(.engineVersion, "")
    }

    private enum CodingKeys: String, CodingKey {
        case name, namespace, pvcNamespace, pvcName, state, robustness, health, replicasDesired, replicasHealthy
        case rebuilding, replicaNodes, node, size, actualSize, lastBackupAt
        case rebuildProgress, backingUp, backupProgress, restoring, restoreProgress, scheduleError, tooManySnapshots
        case snapshots, snapshotsRemoving, backups, backupCount, backupStale, volumeSnapshotsKnown
        case engineImage, engineCurrentImage, engineUpgrading, engineOutdated, engineVersion
    }
}

/// A Longhorn snapshot of a volume (snapshots.longhorn.io), not a CSI VolumeSnapshot.
public struct LonghornSnapshot: Decodable, Equatable, Identifiable, Sendable {
    public let name: String
    /// Unix ms, 0 when Longhorn has none (the base of a restored volume).
    public let createdAt: Int64
    /// Data it holds on top of its parent.
    public let size: Int64
    public let ready: Bool
    /// Asked for (by someone, a job, a CSI snapshot), not taken by Longhorn itself.
    public let userCreated: Bool
    /// A completed backup was taken from it.
    public let backedUp: Bool
    /// The CSI VolumeSnapshot ("namespace/name") it keeps: delete that one, not this.
    public let volumeSnapshot: String
    public let error: String

    public var id: String { name }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.decode(String.self, forKey: .name)
        createdAt = try c.field(.createdAt, 0)
        size = try c.field(.size, 0)
        ready = try c.field(.ready, false)
        userCreated = try c.field(.userCreated, false)
        backedUp = try c.field(.backedUp, false)
        volumeSnapshot = try c.field(.volumeSnapshot, "")
        error = try c.field(.error, "")
    }

    private enum CodingKeys: String, CodingKey { case name, createdAt, size, ready, userCreated, backedUp, volumeSnapshot, error }
}

/// A Longhorn backup of a volume (backups.longhorn.io).
public struct LonghornBackup: Decodable, Equatable, Identifiable, Sendable {
    public let name: String
    public let snapshot: String
    /// Completed, InProgress, Pending, Error or Unknown.
    public let state: String
    public let progress: Int
    /// Unix ms.
    public let createdAt: Int64
    public let size: Int64
    public let error: String

    public var id: String { name }
    public var completed: Bool { state == "Completed" }
    public var failed: Bool { state == "Error" || !error.isEmpty }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.decode(String.self, forKey: .name)
        snapshot = try c.field(.snapshot, "")
        state = try c.field(.state, "")
        progress = try c.field(.progress, 0)
        createdAt = try c.field(.createdAt, 0)
        size = try c.field(.size, 0)
        error = try c.field(.error, "")
    }

    private enum CodingKeys: String, CodingKey { case name, snapshot, state, progress, createdAt, size, error }
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
