import Foundation

// Data services: Garage instances and nodes (see DataServices.swift).

public struct GarageStatus: Decodable, Equatable, Sendable {
    public let error: String
    public let instances: [GarageInstance]
    /// The garage-operator's declarative layer (garage.rajsingh.info), empty when its CRDs are not served.
    public let version: String
    /// A kind reached the Go core's cap: some entries are not shown.
    public let truncated: Bool
    public let clusters: [GarageClusterStatus]
    public let buckets: [GarageBucketStatus]
    public let keys: [GarageKeyStatus]
    /// The operator's GarageNode declarations (not the CLI's nodes above).
    public let nodes: [GarageNodeCr]
    public let `operator`: GarageOperatorStatus?

    public var hasOperator: Bool { !version.isEmpty }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        error = try c.field(.error, "")
        instances = try c.field(.instances, [])
        version = try c.field(.version, "")
        truncated = try c.field(.truncated, false)
        clusters = try c.field(.clusters, [])
        buckets = try c.field(.buckets, [])
        keys = try c.field(.keys, [])
        nodes = try c.field(.nodes, [])
        `operator` = try c.field(.operator, nil)
    }

    private enum CodingKeys: String, CodingKey {
        case error, instances, version, truncated, clusters, buckets, keys, nodes, `operator`
    }
}

public struct GarageInstance: Decodable, Equatable, Identifiable, Sendable {
    public let namespace: String
    public let name: String
    /// Pod the CLI ran in, "" when none was ready.
    public let pod: String
    public let pods: Int
    public let podsReady: Int
    public let version: String
    public let state: GarageState
    /// English, causes first (from the Go core).
    public let message: String
    public let connectedNodes: Int
    public let knownNodes: Int
    public let storageNodes: Int
    public let storageNodesUp: Int
    public let partitions: Int
    public let partitionsQuorum: Int
    public let partitionsAllOk: Int
    /// -1 when unknown.
    public let resyncQueue: Int64
    public let resyncErrors: Int64
    public let tableSyncQueue: Int64
    public let layoutVersion: Int64
    public let nodes: [GarageNode]
    /// cli-json (full picture) or health (status only).
    public let source: String

    public var id: String { label }
    public var label: String { "\(namespace)/\(name)" }
    public var detailed: Bool { source == "cli-json" }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        pod = try c.field(.pod, "")
        pods = try c.field(.pods, 0)
        podsReady = try c.field(.podsReady, 0)
        version = try c.field(.version, "")
        state = GarageState(rawValue: try c.field(.status, "")) ?? .unknown
        message = try c.field(.message, "")
        connectedNodes = try c.field(.connectedNodes, 0)
        knownNodes = try c.field(.knownNodes, 0)
        storageNodes = try c.field(.storageNodes, 0)
        storageNodesUp = try c.field(.storageNodesUp, 0)
        partitions = try c.field(.partitions, 0)
        partitionsQuorum = try c.field(.partitionsQuorum, 0)
        partitionsAllOk = try c.field(.partitionsAllOk, 0)
        resyncQueue = try c.field(.resyncQueue, -1)
        resyncErrors = try c.field(.resyncErrors, -1)
        tableSyncQueue = try c.field(.tableSyncQueue, -1)
        layoutVersion = try c.field(.layoutVersion, 0)
        nodes = try c.field(.nodes, [])
        source = try c.field(.source, "")
    }

    private enum CodingKeys: String, CodingKey {
        case namespace, name, pod, pods, podsReady, version, status, message, connectedNodes, knownNodes, storageNodes
        case storageNodesUp, partitions, partitionsQuorum, partitionsAllOk, resyncQueue, resyncErrors, tableSyncQueue
        case layoutVersion, nodes, source
    }
}

public struct GarageNode: Decodable, Equatable, Identifiable, Sendable {
    public let nodeID: String
    public let hostname: String
    public let zone: String
    public let tags: [String]
    /// Kubernetes node of the pod with that hostname, "" when none.
    public let kubeNode: String
    /// Holds a role in the current layout: only those count as down.
    public let storage: Bool
    public let up: Bool
    /// -1 when up or unknown.
    public let lastSeenSecs: Int64
    public let draining: Bool
    public let dataAvail: Int64
    public let dataTotal: Int64
    public let resyncQueue: Int64
    public let resyncErrors: Int64
    public let tableSyncQueue: Int64
    public let statsError: String
    /// Resync tranquility: 0 resyncs at full speed, 2 is Garage's default; -1 when unknown.
    public let tranquility: Int64

    public var id: String { nodeID.isEmpty ? label : nodeID }

    /// What names the node best: Garage forgets a long-gone node's hostname; its tags often name the host.
    public var label: String {
        if !hostname.isEmpty { return hostname }
        if !tags.isEmpty { return tags.joined(separator: ",") }
        return String(nodeID.prefix(16))
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        nodeID = try c.field(.id, "")
        hostname = try c.field(.hostname, "")
        zone = try c.field(.zone, "")
        tags = try c.field(.tags, [])
        kubeNode = try c.field(.kubeNode, "")
        storage = try c.field(.storage, false)
        up = try c.field(.up, false)
        lastSeenSecs = try c.field(.lastSeenSecs, -1)
        draining = try c.field(.draining, false)
        dataAvail = try c.field(.dataAvail, 0)
        dataTotal = try c.field(.dataTotal, 0)
        resyncQueue = try c.field(.resyncQueue, -1)
        resyncErrors = try c.field(.resyncErrors, -1)
        tableSyncQueue = try c.field(.tableSyncQueue, -1)
        statsError = try c.field(.statsError, "")
        tranquility = try c.field(.tranquility, -1)
    }

    private enum CodingKeys: String, CodingKey {
        case id, hostname, zone, tags, kubeNode, storage, up, lastSeenSecs, draining, dataAvail, dataTotal
        case resyncQueue, resyncErrors, tableSyncQueue, statsError, tranquility
    }
}

public enum GarageState: String, Sendable {
    case healthy, degraded, unavailable, unknown

    public var health: ServiceHealth {
        switch self {
        case .healthy: .ok
        case .degraded: .warning
        case .unavailable: .critical
        case .unknown: .unknown
        }
    }
}

// The garage-operator's declarative layer (rajsinghtech/garage-operator): the custom
// resources of group garage.rajsingh.info, matched to the instances above, and the operator
// Deployment's own health.

public struct GarageOperatorStatus: Decodable, Equatable, Identifiable, Sendable {
    public let namespace: String
    public let name: String
    public let ready: Int
    public let desired: Int
    public let health: ServiceHealth
    /// Known reasons only; values from newer cores are dropped.
    public let reasons: [GarageOpReason]

    public var id: String { label }
    public var label: String { "\(namespace)/\(name)" }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        ready = try c.field(.ready, 0)
        desired = try c.field(.desired, 0)
        health = try c.wire(.health)
        reasons = try c.wireList(.reasons)
    }

    private enum CodingKeys: String, CodingKey { case namespace, name, ready, desired, health, reasons }
}

/// One of the operator's conditions the Go core kept: a problem or a transition.
public struct GarageOpCondition: Decodable, Equatable, Sendable {
    public let type: String
    public let status: String
    public let reason: String
    public let message: String
    /// Unix ms of lastTransitionTime, 0 when unknown.
    public let time: Int64

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        type = try c.field(.type, "")
        status = try c.field(.status, "")
        reason = try c.field(.reason, "")
        message = try c.field(.message, "")
        time = try c.field(.time, 0)
    }

    private enum CodingKeys: String, CodingKey { case type, status, reason, message, time }
}

public struct GarageClusterStatus: Decodable, Equatable, Identifiable, Sendable {
    public let namespace: String
    public let name: String
    /// The garageInstance it drives, "ns/name"; "" when unmatched.
    public let instance: String
    /// Pending..Failed, the operator's own word.
    public let phase: String
    public let clusterID: String
    public let garageVersion: String
    public let replicas: Int
    public let readyReplicas: Int
    public let storageReplicas: Int
    public let storageReadyReplicas: Int
    public let gatewayReplicas: Int
    public let gatewayReadyReplicas: Int
    public let layoutVersion: Int64
    /// 0 when nothing is staged.
    public let stagedLayoutVersion: Int64
    public let pendingGatewayTombstones: [String]
    public let conditions: [GarageOpCondition]
    public let health: ServiceHealth
    /// Known reasons only; values from newer cores are dropped.
    public let reasons: [GarageOpReason]

    public var id: String { label }
    public var label: String { "\(namespace)/\(name)" }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        instance = try c.field(.instance, "")
        phase = try c.field(.phase, "")
        clusterID = try c.field(.clusterId, "")
        garageVersion = try c.field(.garageVersion, "")
        replicas = try c.field(.replicas, 0)
        readyReplicas = try c.field(.readyReplicas, 0)
        storageReplicas = try c.field(.storageReplicas, 0)
        storageReadyReplicas = try c.field(.storageReadyReplicas, 0)
        gatewayReplicas = try c.field(.gatewayReplicas, 0)
        gatewayReadyReplicas = try c.field(.gatewayReadyReplicas, 0)
        layoutVersion = try c.field(.layoutVersion, 0)
        stagedLayoutVersion = try c.field(.stagedLayoutVersion, 0)
        pendingGatewayTombstones = try c.field(.pendingGatewayTombstones, [])
        conditions = try c.field(.conditions, [])
        health = try c.wire(.health)
        reasons = try c.wireList(.reasons)
    }

    private enum CodingKeys: String, CodingKey {
        case namespace, name, instance, phase, clusterId, garageVersion, replicas, readyReplicas
        case storageReplicas, storageReadyReplicas, gatewayReplicas, gatewayReadyReplicas
        case layoutVersion, stagedLayoutVersion, pendingGatewayTombstones, conditions, health, reasons
    }
}

public struct GarageQuotaUsage: Decodable, Equatable, Sendable {
    public let sizeBytes: Int64
    /// 0 = unlimited.
    public let sizeLimit: Int64
    public let sizePercent: Int
    public let objectCount: Int64
    /// 0 = unlimited.
    public let objectLimit: Int64
    public let objectPercent: Int

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        sizeBytes = try c.field(.sizeBytes, 0)
        sizeLimit = try c.field(.sizeLimit, 0)
        sizePercent = try c.field(.sizePercent, 0)
        objectCount = try c.field(.objectCount, 0)
        objectLimit = try c.field(.objectLimit, 0)
        objectPercent = try c.field(.objectPercent, 0)
    }

    private enum CodingKeys: String, CodingKey { case sizeBytes, sizeLimit, sizePercent, objectCount, objectLimit, objectPercent }
}

public struct GarageBucketStatus: Decodable, Equatable, Identifiable, Sendable {
    public let namespace: String
    public let name: String
    public let instance: String
    /// spec.clusterRef.name.
    public let cluster: String
    public let phase: String
    public let bucketID: String
    /// The operator's own wording.
    public let size: String
    public let maxSizeQuota: String
    public let quotaUsage: GarageQuotaUsage?
    public let websiteEnabled: Bool
    public let websiteURL: String
    public let keysWithAccess: Int
    public let conditions: [GarageOpCondition]
    public let health: ServiceHealth
    /// Known reasons only; values from newer cores are dropped.
    public let reasons: [GarageOpReason]

    public var id: String { label }
    public var label: String { "\(namespace)/\(name)" }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        instance = try c.field(.instance, "")
        cluster = try c.field(.cluster, "")
        phase = try c.field(.phase, "")
        bucketID = try c.field(.bucketId, "")
        size = try c.field(.size, "")
        maxSizeQuota = try c.field(.maxSizeQuota, "")
        quotaUsage = try c.field(.quotaUsage, nil)
        websiteEnabled = try c.field(.websiteEnabled, false)
        websiteURL = try c.field(.websiteUrl, "")
        keysWithAccess = try c.field(.keysWithAccess, 0)
        conditions = try c.field(.conditions, [])
        health = try c.wire(.health)
        reasons = try c.wireList(.reasons)
    }

    private enum CodingKeys: String, CodingKey {
        case namespace, name, instance, cluster, phase, bucketId, size, maxSizeQuota, quotaUsage
        case websiteEnabled, websiteUrl, keysWithAccess, conditions, health, reasons
    }
}

public struct GarageKeyAllBuckets: Decodable, Equatable, Sendable {
    public let read: Bool
    public let write: Bool
    public let owner: Bool

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        read = try c.field(.read, false)
        write = try c.field(.write, false)
        owner = try c.field(.owner, false)
    }

    private enum CodingKeys: String, CodingKey { case read, write, owner }
}

public struct GarageKeyPermission: Decodable, Equatable, Sendable {
    public let bucket: String
    public let read: Bool
    public let write: Bool
    public let owner: Bool

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        bucket = try c.field(.bucket, "")
        read = try c.field(.read, false)
        write = try c.field(.write, false)
        owner = try c.field(.owner, false)
    }

    private enum CodingKeys: String, CodingKey { case bucket, read, write, owner }
}

public struct GarageKeyStatus: Decodable, Equatable, Identifiable, Sendable {
    public let namespace: String
    public let name: String
    public let instance: String
    public let cluster: String
    public let phase: String
    /// Garage's internal key id, never the S3 access key.
    public let keyID: String
    public let clusterWide: Bool
    public let allBuckets: GarageKeyAllBuckets?
    /// Capped by the Go core; bucketPermissionsTotal tells how many more there are.
    public let bucketPermissions: [GarageKeyPermission]
    public let bucketPermissionsTotal: Int
    public let health: ServiceHealth
    /// Known reasons only; values from newer cores are dropped.
    public let reasons: [GarageOpReason]

    public var id: String { label }
    public var label: String { "\(namespace)/\(name)" }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        instance = try c.field(.instance, "")
        cluster = try c.field(.cluster, "")
        phase = try c.field(.phase, "")
        keyID = try c.field(.keyId, "")
        clusterWide = try c.field(.clusterWide, false)
        allBuckets = try c.field(.allBuckets, nil)
        bucketPermissions = try c.field(.bucketPermissions, [])
        bucketPermissionsTotal = try c.field(.bucketPermissionsTotal, 0)
        health = try c.wire(.health)
        reasons = try c.wireList(.reasons)
    }

    private enum CodingKeys: String, CodingKey {
        case namespace, name, instance, cluster, phase, keyId, clusterWide, allBuckets
        case bucketPermissions, bucketPermissionsTotal, health, reasons
    }
}

/// The operator's GarageNode declaration: one durable Garage identity and layout role.
public struct GarageNodeCr: Decodable, Equatable, Identifiable, Sendable {
    public let namespace: String
    public let name: String
    public let instance: String
    public let cluster: String
    public let phase: String
    public let nodeID: String
    /// The effective zone (spec.zone or zoneFrom).
    public let zone: String
    public let capacity: String
    public let gateway: Bool
    public let connected: Bool
    public let inLayout: Bool
    /// Seconds ago, -1 when connected or unknown.
    public let lastSeenSecs: Int64
    public let health: ServiceHealth
    /// Known reasons only; values from newer cores are dropped.
    public let reasons: [GarageOpReason]

    public var id: String { label }
    public var label: String { "\(namespace)/\(name)" }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        instance = try c.field(.instance, "")
        cluster = try c.field(.cluster, "")
        phase = try c.field(.phase, "")
        nodeID = try c.field(.nodeId, "")
        zone = try c.field(.zone, "")
        capacity = try c.field(.capacity, "")
        gateway = try c.field(.gateway, false)
        connected = try c.field(.connected, false)
        inLayout = try c.field(.inLayout, false)
        lastSeenSecs = try c.field(.lastSeenSecs, -1)
        health = try c.wire(.health)
        reasons = try c.wireList(.reasons)
    }

    private enum CodingKeys: String, CodingKey {
        case namespace, name, instance, cluster, phase, nodeId, zone, capacity, gateway
        case connected, inLayout, lastSeenSecs, health, reasons
    }
}

/// Why a garage-operator item is not ok, as the Go core names it.
public enum GarageOpReason: String, Sendable {
    case clusterFailed, adminUnreachable, operatorDown, degraded, storageReplicas, quorumAtRisk
    case layoutStaged, tombstones, nodeDisconnected, nodeNotInLayout
    case bucketFailed, lifecycleNotApplied, lookupStuck, metadataDegraded, deletionBlocked, websiteExposure
    case keyFailed
}
