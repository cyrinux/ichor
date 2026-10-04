import Foundation

// Mirrors go/ichorgo/kube_dataservices.go, kube_longhorn.go, kube_garage.go and kube_cnpg.go
// (the wire format is documented in plans/data-services/README.md).

/// Health of the storage and database operators a cluster runs; a nil section is not installed.
public struct DataServices: Decodable, Equatable, Sendable {
    public let longhorn: LonghornStatus?
    public let garage: GarageStatus?
    public let cnpg: CnpgStatus?
    public let dragonfly: DragonflyStatus?

    public init(longhorn: LonghornStatus? = nil, garage: GarageStatus? = nil, cnpg: CnpgStatus? = nil, dragonfly: DragonflyStatus? = nil) {
        self.longhorn = longhorn
        self.garage = garage
        self.cnpg = cnpg
        self.dragonfly = dragonfly
    }
}

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
        health = ServiceHealth(wire: try c.field(.health, ""))
        reasons = (try c.field(.reasons, [String]())).compactMap(DragonflyReason.init(rawValue:))
        replicas = try c.field(.replicas, 0)
        readyPods = try c.field(.readyPods, 0)
        master = try c.field(.master, "")
        pods = try c.field(.pods, [])
    }

    private enum CodingKeys: String, CodingKey { case namespace, name, phase, health, reasons, replicas, readyPods, master, pods }
}

public struct DragonflyPod: Decodable, Equatable, Identifiable, Sendable {
    public let name: String
    public let node: String
    public let phase: String
    /// The operator's role label: master or replica.
    public let role: String
    public let ready: Bool

    public var id: String { name }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.decode(String.self, forKey: .name)
        node = try c.field(.node, "")
        phase = try c.field(.phase, "")
        role = try c.field(.role, "")
        ready = try c.field(.ready, false)
    }

    private enum CodingKeys: String, CodingKey { case name, node, phase, role, ready }
}

/// Why a Dragonfly instance is not ok, as the Go core names it.
public enum DragonflyReason: String, Sendable {
    case noReady, noMaster, masters, pods, notReady
}

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
        health = ServiceHealth(wire: try c.field(.health, ""))
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

public struct GarageStatus: Decodable, Equatable, Sendable {
    public let error: String
    public let instances: [GarageInstance]

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        error = try c.field(.error, "")
        instances = try c.field(.instances, [])
    }

    private enum CodingKeys: String, CodingKey { case error, instances }
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
        health = ServiceHealth(wire: try c.field(.health, ""))
        hibernated = try c.field(.hibernated, false)
        reasons = (try c.field(.reasons, [String]())).compactMap(CnpgReason.init(rawValue:))
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

public struct CnpgPod: Decodable, Equatable, Identifiable, Sendable {
    public let name: String
    /// "" while Pending: not scheduled anywhere.
    public let node: String
    public let phase: String
    /// primary or replica, "" when not running.
    public let role: String
    public let ready: Bool

    public var id: String { name }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.decode(String.self, forKey: .name)
        node = try c.field(.node, "")
        phase = try c.field(.phase, "")
        role = try c.field(.role, "")
        ready = try c.field(.ready, false)
    }

    private enum CodingKeys: String, CodingKey { case name, node, phase, role, ready }
}

/// Health of one item (a volume, a Postgres cluster), worst first.
public enum ServiceHealth: Int, Sendable, Comparable, CaseIterable {
    case critical, warning, unknown, ok, idle

    public init(wire: String) {
        switch wire {
        case "critical": self = .critical
        case "warning": self = .warning
        case "ok": self = .ok
        case "idle": self = .idle
        default: self = .unknown
        }
    }

    public var needsAttention: Bool { self == .critical || self == .warning }

    public static func < (a: ServiceHealth, b: ServiceHealth) -> Bool { a.rawValue < b.rawValue }

    /// The worst of healths, ok when there is none (or only idle ones).
    public static func worst<S: Sequence>(_ healths: S) -> ServiceHealth where S.Element == ServiceHealth {
        guard let worst = healths.min(), worst != .idle else { return .ok }
        return worst
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

/// Why a Postgres cluster is not ok, as the Go core names it.
public enum CnpgReason: String, Sendable {
    case noInstance, failover, instances, switchover, notReady, archiving, backupFailed, backupStale
}

/// The systems, in display order, with their app catalog id.
public enum DataServiceKind: String, Sendable, CaseIterable, Identifiable, Hashable {
    case longhorn = "longhorn"
    case garage = "garage"
    case cnpg = "cloudnative-pg"
    case dragonfly = "dragonfly"

    public var id: String { rawValue }
    public var catalogID: String { rawValue }

    /// Product names: never translated.
    public var title: String {
        switch self {
        case .longhorn: "Longhorn"
        case .garage: "Garage"
        case .cnpg: "CloudNativePG"
        case .dragonfly: "Dragonfly"
        }
    }

    /// The segmented picker's name: short enough for four segments.
    public var tabTitle: String { self == .cnpg ? "CNPG" : title }
}

/// The catalog ids among the inventory's apps, for KubeDataServices: "" when none runs.
public func dataServiceHints(_ inventory: ClusterInventory) -> String {
    let ids = Set(inventory.apps.map(\.id))
    return DataServiceKind.allCases.map(\.catalogID).filter(ids.contains).joined(separator: ",")
}

/// One system at a glance: items, how many need attention, the worst health, the error if unreadable.
public struct ServiceSummary: Equatable, Sendable {
    public let total: Int
    public let attention: Int
    public let health: ServiceHealth
    public let error: String

    public init(total: Int, attention: Int, health: ServiceHealth, error: String = "") {
        self.total = total
        self.attention = attention
        self.health = health
        self.error = error
    }
}

/// A node that is not ready and the problems it most likely explains.
public struct LikelyCause: Equatable, Sendable, Identifiable {
    public let node: String
    public let problems: Int
    public var id: String { node }

    public init(node: String, problems: Int) {
        self.node = node
        self.problems = problems
    }
}

public extension DataServices {
    /// The installed systems, in display order.
    var detected: [DataServiceKind] {
        [longhorn != nil ? .longhorn : nil, garage != nil ? .garage : nil, cnpg != nil ? .cnpg : nil,
         dragonfly != nil ? .dragonfly : nil].compactMap { $0 }
    }

    func summary(_ kind: DataServiceKind) -> ServiceSummary? {
        switch kind {
        case .longhorn:
            guard let lh = longhorn else { return nil }
            if !lh.error.isEmpty { return ServiceSummary(total: lh.volumes.count, attention: 0, health: .unknown, error: lh.error) }
            // A node that is not ready or a backup target that is gone needs a look as much as a volume.
            let healths = lh.volumes.map(\.health) + lh.nodes.filter { !$0.ready }.map { _ in ServiceHealth.warning } +
                lh.backupTargets.filter { !$0.available }.map { _ in ServiceHealth.warning }
            return ServiceSummary(total: lh.volumes.count, attention: healths.filter(\.needsAttention).count, health: .worst(healths))
        case .garage:
            guard let g = garage else { return nil }
            if !g.error.isEmpty { return ServiceSummary(total: g.instances.count, attention: 0, health: .unknown, error: g.error) }
            let healths = g.instances.map(\.state.health)
            return ServiceSummary(total: g.instances.count, attention: healths.filter(\.needsAttention).count, health: .worst(healths))
        case .cnpg:
            guard let p = cnpg else { return nil }
            if !p.error.isEmpty && p.clusters.isEmpty { return ServiceSummary(total: 0, attention: 0, health: .unknown, error: p.error) }
            let healths = p.clusters.map(\.health)
            return ServiceSummary(total: p.clusters.count, attention: healths.filter(\.needsAttention).count, health: .worst(healths), error: p.error)
        case .dragonfly:
            guard let d = dragonfly else { return nil }
            if !d.error.isEmpty && d.instances.isEmpty { return ServiceSummary(total: 0, attention: 0, health: .unknown, error: d.error) }
            let healths = d.instances.map(\.health)
            return ServiceSummary(total: d.instances.count, attention: healths.filter(\.needsAttention).count, health: .worst(healths), error: d.error)
        }
    }

    /// Worst health over every installed system.
    var worst: ServiceHealth { .worst(detected.compactMap { summary($0)?.health }) }

    /// Nodes that are not ready (downNodes, plus the ones Longhorn reports) and how many problems each
    /// explains: a volume with a replica there, a Garage cluster whose missing node runs there, a Postgres
    /// cluster with an instance there that is not ready. Worst first; empty when no problem points to a node.
    func likelyCauses(downNodes: Set<String> = []) -> [LikelyCause] {
        let down = downNodes.union((longhorn?.nodes ?? []).filter { !$0.ready }.map(\.name))
        guard !down.isEmpty else { return [] }
        var hits: [String: Int] = [:]
        func count(_ nodes: [String]) { for node in Set(nodes) where down.contains(node) { hits[node, default: 0] += 1 } }

        for volume in longhorn?.volumes ?? [] where volume.health.needsAttention { count(volume.replicaNodes) }
        for inst in garage?.instances ?? [] where inst.state.health.needsAttention {
            count(inst.nodes.filter { !$0.up && $0.storage }.flatMap { [$0.kubeNode] + $0.tags })
        }
        for cluster in cnpg?.clusters ?? [] where cluster.health.needsAttention {
            count(cluster.instancePods.filter { !$0.ready }.map(\.node))
        }
        for instance in dragonfly?.instances ?? [] where instance.health.needsAttention {
            count(instance.pods.filter { !$0.ready }.map(\.node))
        }
        return hits.map { LikelyCause(node: $0.key, problems: $0.value) }
            .sorted { $0.problems != $1.problems ? $0.problems > $1.problems : $0.node < $1.node }
    }
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

/// Clusters matching query by namespace/name, only those needing attention when problemsOnly.
public func filterClusters(_ clusters: [CnpgCluster], problemsOnly: Bool, query: String) -> [CnpgCluster] {
    let q = query.trimmingCharacters(in: .whitespaces)
    return clusters.filter { c in
        (!problemsOnly || c.health.needsAttention) && (q.isEmpty || c.label.localizedCaseInsensitiveContains(q))
    }
}
