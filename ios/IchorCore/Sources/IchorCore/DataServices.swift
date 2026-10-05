import Foundation

// Mirrors go/ichorgo/kube_dataservices.go, kube_longhorn.go, kube_garage.go, kube_cnpg.go and kube_certmanager.go
// (the wire format is documented in plans/data-services/README.md).

/// Health of the storage and database operators a cluster runs; a nil section is not installed.
public struct DataServices: Decodable, Equatable, Sendable {
    public let longhorn: LonghornStatus?
    public let garage: GarageStatus?
    public let cnpg: CnpgStatus?
    public let dragonfly: DragonflyStatus?
    public let mariadb: MariaDbStatus?
    public let percona: PerconaStatus?
    public let certManager: CertManagerStatus?
    public let velero: VeleroStatus?
    public let ceph: CephStatus?

    public init(longhorn: LonghornStatus? = nil, garage: GarageStatus? = nil, cnpg: CnpgStatus? = nil, dragonfly: DragonflyStatus? = nil,
                mariadb: MariaDbStatus? = nil, percona: PerconaStatus? = nil, certManager: CertManagerStatus? = nil,
                velero: VeleroStatus? = nil, ceph: CephStatus? = nil) {
        self.longhorn = longhorn
        self.garage = garage
        self.cnpg = cnpg
        self.dragonfly = dragonfly
        self.mariadb = mariadb
        self.percona = percona
        self.certManager = certManager
        self.velero = velero
        self.ceph = ceph
    }
}

public struct CertManagerStatus: Decodable, Equatable, Sendable {
    public let version: String
    public let error: String
    /// Worst first, then the soonest expiry.
    public let certificates: [Certificate]
    public let issuers: [CertIssuer]

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        version = try c.field(.version, "")
        error = try c.field(.error, "")
        certificates = try c.field(.certificates, [])
        issuers = try c.field(.issuers, [])
    }

    private enum CodingKeys: String, CodingKey { case version, error, certificates, issuers }
}

public struct Certificate: Decodable, Equatable, Identifiable, Sendable {
    public let namespace: String
    public let name: String
    public let secretName: String
    /// The common name then the DNS names, the first few; dnsNameCount counts them all.
    public let dnsNames: [String]
    public let dnsNameCount: Int
    /// "ClusterIssuer/letsencrypt", "Issuer/internal-ca".
    public let issuer: String
    public let health: ServiceHealth
    /// Known reasons only; values from newer cores are dropped.
    public let reasons: [CertReason]
    public let ready: Bool
    /// cert-manager is issuing it now (a renewal, or one forced from the app).
    public let issuing: Bool
    /// The Ready condition's message when not ready.
    public let message: String
    /// Unix ms, 0 before the first issuance.
    public let notAfter: Int64
    /// Unix ms, 0 when no renewal is planned.
    public let renewalTime: Int64
    public let failedAttempts: Int

    public var id: String { label }
    public var label: String { "\(namespace)/\(name)" }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        secretName = try c.field(.secretName, "")
        dnsNames = try c.field(.dnsNames, [])
        dnsNameCount = try c.field(.dnsNameCount, 0)
        issuer = try c.field(.issuer, "")
        health = ServiceHealth(wire: try c.field(.health, ""))
        reasons = (try c.field(.reasons, [String]())).compactMap(CertReason.init(rawValue:))
        ready = try c.field(.ready, false)
        issuing = try c.field(.issuing, false)
        message = try c.field(.message, "")
        notAfter = try c.field(.notAfter, 0)
        renewalTime = try c.field(.renewalTime, 0)
        failedAttempts = try c.field(.failedAttempts, 0)
    }

    private enum CodingKeys: String, CodingKey {
        case namespace, name, secretName, dnsNames, dnsNameCount, issuer, health, reasons, ready, issuing, message, notAfter, renewalTime, failedAttempts
    }
}

public struct CertIssuer: Decodable, Equatable, Identifiable, Sendable {
    /// Issuer or ClusterIssuer.
    public let kind: String
    /// "" for a ClusterIssuer.
    public let namespace: String
    public let name: String
    /// acme, ca, selfSigned, vault or venafi; "" for another.
    public let type: String
    /// The ACME server's host.
    public let server: String
    public let ready: Bool
    public let message: String
    public let health: ServiceHealth

    public var id: String { label }
    /// "ClusterIssuer/letsencrypt", "Issuer/app/internal-ca": unique across both kinds.
    public var label: String { [kind, namespace, name].filter { !$0.isEmpty }.joined(separator: "/") }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        kind = try c.field(.kind, "")
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        type = try c.field(.type, "")
        server = try c.field(.server, "")
        ready = try c.field(.ready, false)
        message = try c.field(.message, "")
        health = ServiceHealth(wire: try c.field(.health, ""))
    }

    private enum CodingKeys: String, CodingKey { case kind, namespace, name, type, server, ready, message, health }
}

/// Why a certificate is not ok, as the Go core names it.
public enum CertReason: String, Sendable {
    case expired, expiring, renewalOverdue, notReady, issuer
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
public struct VeleroStatus: Decodable, Equatable, Sendable {
    public let version: String
    public let error: String
    /// Problems first.
    public let schedules: [VeleroSchedule]
    /// Backups taken by hand (no schedule) that failed in the last week, newest first.
    public let adhoc: [VeleroAdhocBackup]
    /// Unavailable first.
    public let locations: [VeleroLocation]

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        version = try c.field(.version, "")
        error = try c.field(.error, "")
        schedules = try c.field(.schedules, [])
        adhoc = try c.field(.adhoc, [])
        locations = try c.field(.locations, [])
    }

    private enum CodingKeys: String, CodingKey { case version, error, schedules, adhoc, locations }
}

public struct VeleroSchedule: Decodable, Equatable, Identifiable, Sendable {
    public let namespace: String
    public let name: String
    /// Cron, as written.
    public let schedule: String
    public let paused: Bool
    /// The schedule's own: New, Enabled or FailedValidation.
    public let phase: String
    public let validationErrors: [String]
    public let health: ServiceHealth
    /// Known reasons only; values from newer cores are dropped.
    public let reasons: [VeleroReason]
    public let storageLocation: String
    /// Empty or "*": every namespace.
    public let includedNamespaces: [String]
    /// The latest finished backup, nil when none is left.
    public let lastBackup: VeleroBackup?
    /// The latest Completed backup (unix ms), 0 when none.
    public let lastSuccessAt: Int64
    public let inProgress: Bool

    public var id: String { label }
    public var label: String { "\(namespace)/\(name)" }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        schedule = try c.field(.schedule, "")
        paused = try c.field(.paused, false)
        phase = try c.field(.phase, "")
        validationErrors = try c.field(.validationErrors, [])
        health = ServiceHealth(wire: try c.field(.health, ""))
        reasons = (try c.field(.reasons, [String]())).compactMap(VeleroReason.init(rawValue:))
        storageLocation = try c.field(.storageLocation, "")
        includedNamespaces = try c.field(.includedNamespaces, [])
        lastBackup = try c.decodeIfPresent(VeleroBackup.self, forKey: .lastBackup)
        lastSuccessAt = try c.field(.lastSuccessAt, 0)
        inProgress = try c.field(.inProgress, false)
    }

    private enum CodingKeys: String, CodingKey {
        case namespace, name, schedule, paused, phase, validationErrors, health, reasons, storageLocation, includedNamespaces
        case lastBackup, lastSuccessAt, inProgress
    }
}

public struct VeleroBackup: Decodable, Equatable, Sendable {
    public let name: String
    /// Completed, PartiallyFailed, Failed or FailedValidation.
    public let phase: String
    public let startedAt: Int64
    public let completedAt: Int64
    public let errors: Int
    public let warnings: Int
    public let failureReason: String

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        phase = try c.field(.phase, "")
        startedAt = try c.field(.startedAt, 0)
        completedAt = try c.field(.completedAt, 0)
        errors = try c.field(.errors, 0)
        warnings = try c.field(.warnings, 0)
        failureReason = try c.field(.failureReason, "")
    }

    private enum CodingKeys: String, CodingKey { case name, phase, startedAt, completedAt, errors, warnings, failureReason }
}

/// A backup taken by hand that failed: a warning.
public struct VeleroAdhocBackup: Decodable, Equatable, Identifiable, Sendable {
    public let namespace: String
    public let backup: VeleroBackup
    public let storageLocation: String
    public let health: ServiceHealth

    public var id: String { label }
    public var label: String { "\(namespace)/\(backup.name)" }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        // The Go core inlines the backup's fields.
        backup = try VeleroBackup(from: decoder)
        storageLocation = try c.field(.storageLocation, "")
        health = ServiceHealth(wire: try c.field(.health, ""))
    }

    private enum CodingKeys: String, CodingKey { case namespace, storageLocation, health }
}

public struct VeleroLocation: Decodable, Equatable, Identifiable, Sendable {
    public let namespace: String
    public let name: String
    public let provider: String
    public let bucket: String
    public let isDefault: Bool
    /// Available or Unavailable, "" before the first check.
    public let phase: String
    public let message: String
    public let lastValidatedAt: Int64
    public let health: ServiceHealth

    public var id: String { label }
    public var label: String { "\(namespace)/\(name)" }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        provider = try c.field(.provider, "")
        bucket = try c.field(.bucket, "")
        isDefault = try c.field(.isDefault, false)
        phase = try c.field(.phase, "")
        message = try c.field(.message, "")
        lastValidatedAt = try c.field(.lastValidatedAt, 0)
        health = ServiceHealth(wire: try c.field(.health, ""))
    }

    private enum CodingKeys: String, CodingKey {
        case namespace, name, provider, bucket, phase, message, lastValidatedAt, health
        case isDefault = "default"
    }
}

/// Why a Velero schedule is not ok, as the Go core names it.
public enum VeleroReason: String, Sendable {
    case failed, location, partiallyFailed, stale, invalid
}


public struct PerconaStatus: Decodable, Equatable, Sendable {
    public let version: String
    public let error: String
    public let clusters: [PerconaCluster]

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        version = try c.field(.version, "")
        error = try c.field(.error, "")
        clusters = try c.field(.clusters, [])
    }

    private enum CodingKeys: String, CodingKey { case version, error, clusters }
}

public struct PerconaCluster: Decodable, Equatable, Identifiable, Sendable {
    public let namespace: String
    public let name: String
    /// The operator's own word: ready, initializing, paused, stopping, error or unknown.
    public let state: String
    /// The operator's messages, "; "-joined.
    public let message: String
    public let crVersion: String
    public let paused: Bool
    public let health: ServiceHealth
    /// Known reasons only; values from newer cores are dropped.
    public let reasons: [PerconaReason]
    public let pxcSize: Int
    public let pxcReady: Int
    /// haproxy, proxysql or "" for none.
    public let proxy: String
    public let proxySize: Int
    public let proxyReady: Int
    /// The PXC members, by name.
    public let pods: [PerconaPod]
    public let lastBackupAt: Int64
    public let lastBackupFailedAt: Int64
    public let backupSchedules: [PerconaSchedule]

    public var id: String { label }
    public var label: String { "\(namespace)/\(name)" }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        state = try c.field(.state, "")
        message = try c.field(.message, "")
        crVersion = try c.field(.crVersion, "")
        paused = try c.field(.paused, false)
        health = ServiceHealth(wire: try c.field(.health, ""))
        reasons = (try c.field(.reasons, [String]())).compactMap(PerconaReason.init(rawValue:))
        pxcSize = try c.field(.pxcSize, 0)
        pxcReady = try c.field(.pxcReady, 0)
        proxy = try c.field(.proxy, "")
        proxySize = try c.field(.proxySize, 0)
        proxyReady = try c.field(.proxyReady, 0)
        pods = try c.field(.pods, [])
        lastBackupAt = try c.field(.lastBackupAt, 0)
        lastBackupFailedAt = try c.field(.lastBackupFailedAt, 0)
        backupSchedules = try c.field(.backupSchedules, [])
    }

    private enum CodingKeys: String, CodingKey {
        case namespace, name, state, message, crVersion, paused, health, reasons, pxcSize, pxcReady, proxy, proxySize, proxyReady, pods
        case lastBackupAt, lastBackupFailedAt, backupSchedules
    }
}

public struct PerconaPod: Decodable, Equatable, Identifiable, Sendable {
    public let name: String
    /// "" while Pending: not scheduled anywhere.
    public let node: String
    public let phase: String
    public let ready: Bool

    public var id: String { name }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.decode(String.self, forKey: .name)
        node = try c.field(.node, "")
        phase = try c.field(.phase, "")
        ready = try c.field(.ready, false)
    }

    private enum CodingKeys: String, CodingKey { case name, node, phase, ready }
}

public struct PerconaSchedule: Decodable, Equatable, Sendable {
    public let name: String
    public let schedule: String
    public let keep: Int
    public let storageName: String

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        schedule = try c.field(.schedule, "")
        keep = try c.field(.keep, 0)
        storageName = try c.field(.storageName, "")
    }

    private enum CodingKeys: String, CodingKey { case name, schedule, keep, storageName }
}

/// Why a Percona XtraDB cluster is not ok, as the Go core names it.
public enum PerconaReason: String, Sendable {
    case error, noMember, members, proxy, initializing, backupFailed, backupStale
}

public struct CephStatus: Decodable, Equatable, Sendable {
    public let version: String
    public let error: String
    public let clusters: [CephCluster]
    public let pools: [CephPool]
    /// By namespace, then OSD number.
    public let osds: [CephOSD]

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        version = try c.field(.version, "")
        error = try c.field(.error, "")
        clusters = try c.field(.clusters, [])
        pools = try c.field(.pools, [])
        osds = try c.field(.osds, [])
    }

    private enum CodingKeys: String, CodingKey { case version, error, clusters, pools, osds }
}

public struct CephCluster: Decodable, Equatable, Identifiable, Sendable {
    public let namespace: String
    public let name: String
    /// Rook's own word: Ready (Connected when external), Progressing, Failure...
    public let phase: String
    public let message: String
    /// HEALTH_OK, HEALTH_WARN or HEALTH_ERR; "" before Ceph reported.
    public let cephHealth: String
    public let health: ServiceHealth
    /// Known reasons only; values from newer cores are dropped.
    public let reasons: [CephReason]
    /// Ceph's health checks, errors first.
    public let checks: [CephCheck]
    public let bytesTotal: Int64
    public let bytesUsed: Int64
    public let osdsUp: Int
    public let osdsTotal: Int
    public let monsReady: Int
    public let monsTotal: Int
    /// Nodes of its OSD and mon pods that are not ready.
    public let notReadyNodes: [String]
    /// Ceph's version, e.g. 19.2.3-0.
    public let version: String
    public let external: Bool

    public var id: String { label }
    public var label: String { "\(namespace)/\(name)" }
    /// Raw capacity used, 0 when Ceph has not reported it.
    public var usedFraction: Double { bytesTotal > 0 ? Double(bytesUsed) / Double(bytesTotal) : 0 }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        phase = try c.field(.phase, "")
        message = try c.field(.message, "")
        cephHealth = try c.field(.cephHealth, "")
        health = ServiceHealth(wire: try c.field(.health, ""))
        reasons = (try c.field(.reasons, [String]())).compactMap(CephReason.init(rawValue:))
        checks = try c.field(.checks, [])
        bytesTotal = try c.field(.bytesTotal, 0)
        bytesUsed = try c.field(.bytesUsed, 0)
        osdsUp = try c.field(.osdsUp, 0)
        osdsTotal = try c.field(.osdsTotal, 0)
        monsReady = try c.field(.monsReady, 0)
        monsTotal = try c.field(.monsTotal, 0)
        notReadyNodes = try c.field(.notReadyNodes, [])
        version = try c.field(.version, "")
        external = try c.field(.external, false)
    }

    private enum CodingKeys: String, CodingKey {
        case namespace, name, phase, message, cephHealth, health, reasons, checks, bytesTotal, bytesUsed
        case osdsUp, osdsTotal, monsReady, monsTotal, notReadyNodes, version, external
    }
}

public struct CephCheck: Decodable, Equatable, Identifiable, Sendable {
    /// MON_DOWN, OSD_NEARFULL...
    public let name: String
    /// HEALTH_WARN or HEALTH_ERR.
    public let severity: String
    public let message: String

    public var id: String { name }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.decode(String.self, forKey: .name)
        severity = try c.field(.severity, "")
        message = try c.field(.message, "")
    }

    private enum CodingKeys: String, CodingKey { case name, severity, message }
}

/// A block pool, a filesystem or an object store.
public struct CephPool: Decodable, Equatable, Identifiable, Sendable {
    public let namespace: String
    public let name: String
    /// blockPool, filesystem or objectStore.
    public let kind: String
    public let phase: String
    public let health: ServiceHealth

    public var id: String { "\(kind)/\(label)" }
    public var label: String { "\(namespace)/\(name)" }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        kind = try c.field(.kind, "")
        phase = try c.field(.phase, "")
        health = ServiceHealth(wire: try c.field(.health, ""))
    }

    private enum CodingKeys: String, CodingKey { case namespace, name, kind, phase, health }
}

public struct CephOSD: Decodable, Equatable, Identifiable, Sendable {
    /// The cluster's.
    public let namespace: String
    /// The ceph-osd-id label ("id" on the wire).
    public let osdID: String
    public let pod: String
    public let node: String
    public let phase: String
    public let ready: Bool

    public var id: String { "\(namespace)/\(pod)" }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        osdID = try c.field(.osdID, "")
        pod = try c.field(.pod, "")
        node = try c.field(.node, "")
        phase = try c.field(.phase, "")
        ready = try c.field(.ready, false)
    }

    private enum CodingKeys: String, CodingKey {
        case namespace, pod, node, phase, ready
        case osdID = "id"
    }
}

/// Why a Ceph cluster is not ok, as the Go core names it.
public enum CephReason: String, Sendable {
    case healthErr, failure, full, noOSD, noQuorum, healthWarn, nearFull, osds, mons, notReady
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
    case mariadb = "mariadb"
    case percona = "percona-xtradb"
    case certManager = "cert-manager"
    case velero = "velero"
    case ceph = "rook"

    public var id: String { rawValue }
    public var catalogID: String { rawValue }

    /// Product names: never translated.
    public var title: String {
        switch self {
        case .longhorn: "Longhorn"
        case .garage: "Garage"
        case .cnpg: "CloudNativePG"
        case .dragonfly: "Dragonfly"
        case .mariadb: "MariaDB"
        case .percona: "Percona XtraDB Cluster"
        case .certManager: "cert-manager"
        case .velero: "Velero"
        case .ceph: "Rook Ceph"
        }
    }

    /// The segmented picker's name: short enough for four segments.
    public var tabTitle: String {
        switch self {
        case .cnpg: "CNPG"
        case .percona: "Percona"
        default: title
        }
    }
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
         dragonfly != nil ? .dragonfly : nil, mariadb != nil ? .mariadb : nil, percona != nil ? .percona : nil,
         certManager != nil ? .certManager : nil, velero != nil ? .velero : nil,
         ceph != nil ? .ceph : nil].compactMap { $0 }
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
        case .mariadb:
            return mariadb?.summary
        case .percona:
            guard let p = percona else { return nil }
            if !p.error.isEmpty && p.clusters.isEmpty { return ServiceSummary(total: 0, attention: 0, health: .unknown, error: p.error) }
            let healths = p.clusters.map(\.health)
            return ServiceSummary(total: p.clusters.count, attention: healths.filter(\.needsAttention).count, health: .worst(healths), error: p.error)
        case .certManager:
            guard let cm = certManager else { return nil }
            if !cm.error.isEmpty && cm.certificates.isEmpty { return ServiceSummary(total: 0, attention: 0, health: .unknown, error: cm.error) }
            // An issuer that is not ready needs a look as much as a certificate.
            let healths = cm.certificates.map(\.health) + cm.issuers.map(\.health)
            return ServiceSummary(total: cm.certificates.count, attention: healths.filter(\.needsAttention).count, health: .worst(healths), error: cm.error)
        case .velero:
            // The total counts the schedules; a storage location down or a failed backup taken by hand needs a look too.
            guard let v = velero else { return nil }
            if !v.error.isEmpty && v.schedules.isEmpty && v.locations.isEmpty { return ServiceSummary(total: 0, attention: 0, health: .unknown, error: v.error) }
            let healths = v.schedules.map(\.health) + v.locations.map(\.health) + v.adhoc.map(\.health)
            return ServiceSummary(total: v.schedules.count, attention: healths.filter(\.needsAttention).count, health: .worst(healths), error: v.error)
        case .ceph:
            guard let c = ceph else { return nil }
            if !c.error.isEmpty && c.clusters.isEmpty { return ServiceSummary(total: 0, attention: 0, health: .unknown, error: c.error) }
            // Clusters are the items; a pool that is not ready needs a look as much as a cluster.
            let healths = c.clusters.map(\.health) + c.pools.map(\.health)
            return ServiceSummary(total: c.clusters.count, attention: healths.filter(\.needsAttention).count, health: .worst(healths), error: c.error)
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
        for cluster in mariadb?.clusters ?? [] where cluster.health.needsAttention {
            count(cluster.pods.filter { !$0.ready }.map(\.node))
        }
        for cluster in percona?.clusters ?? [] where cluster.health.needsAttention {
            count(cluster.pods.filter { !$0.ready }.map(\.node))
        }
        for cluster in ceph?.clusters ?? [] where cluster.health.needsAttention { count(cluster.notReadyNodes) }
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
