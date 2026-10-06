package name.levis.ichor.model

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/kube_dataservices.go, kube_longhorn.go, kube_garage.go and kube_cnpg.go
// (the wire format is documented in plans/data-services/README.md).

/** Health of the storage and database operators a cluster runs; a null section is not installed. */
@Serializable
data class DataServices(
    val longhorn: LonghornStatus? = null,
    val garage: GarageStatus? = null,
    val cnpg: CnpgStatus? = null,
    val dragonfly: DragonflyStatus? = null,
    val mariadb: MariaDbStatus? = null,
    val percona: PerconaStatus? = null,
    val certManager: CertManagerStatus? = null,
    val velero: VeleroStatus? = null,
    val ceph: CephStatus? = null,
)

@Serializable
data class LonghornStatus(
    val version: String = "",
    /** Installed but could not be read. */
    val error: String = "",
    val volumes: List<LonghornVolume> = emptyList(),
    val nodes: List<LonghornNode> = emptyList(),
    val backupTargets: List<LonghornBackupTarget> = emptyList(),
)

@Serializable
data class LonghornVolume(
    val name: String,
    val namespace: String = "",
    /** "" when no claim is bound. */
    val pvcNamespace: String = "",
    val pvcName: String = "",
    /** creating, attached, detached, attaching, detaching or deleting. */
    val state: String = "",
    /** healthy, degraded, faulted or unknown. */
    val robustness: String = "",
    val health: String = "",
    val replicasDesired: Int = 0,
    val replicasHealthy: Int = 0,
    val rebuilding: Int = 0,
    /** Nodes holding a replica, failed ones too. */
    val replicaNodes: List<String> = emptyList(),
    /** Where it is attached. */
    val node: String = "",
    val size: Long = 0,
    val actualSize: Long = 0,
    /** Unix millis, 0 when never. */
    val lastBackupAt: Long = 0,
    /** Slowest replica rebuild, 0-100, with [rebuilding] > 0. */
    val rebuildProgress: Int = 0,
    val backingUp: Boolean = false,
    val backupProgress: Int = 0,
    val restoring: Boolean = false,
    val restoreProgress: Int = 0,
    /** Why a replica cannot be placed (English, from Longhorn), "" when it can. */
    val scheduleError: String = "",
    /** Close to Longhorn's snapshot limit. */
    val tooManySnapshots: Boolean = false,
) {
    val serviceHealth: ServiceHealth get() = ServiceHealth.from(health)

    val attached: Boolean get() = state == "attached"

    /** The claim it backs ("namespace/name"), or the volume's own name when unbound. */
    val label: String get() = if (pvcName.isNotEmpty()) "$pvcNamespace/$pvcName" else name
}

@Serializable
data class LonghornNode(
    val name: String,
    /** Longhorn's own, where the node object lives. */
    val namespace: String = "",
    val ready: Boolean = false,
    val schedulable: Boolean = false,
    /** What the user asked: new replicas on the node, its replicas moved away. */
    val allowScheduling: Boolean = false,
    val evictionRequested: Boolean = false,
    /** Replicas it holds, failed ones too. */
    val replicas: Int = 0,
    val disks: List<LonghornDisk> = emptyList(),
)

@Serializable
data class LonghornDisk(
    val path: String = "",
    val schedulable: Boolean = false,
    val available: Long = 0,
    val maximum: Long = 0,
    val scheduled: Long = 0,
)

@Serializable
data class LonghornBackupTarget(
    val name: String = "",
    val url: String = "",
    val available: Boolean = false,
    val message: String = "",
)

@Serializable
data class GarageStatus(
    val error: String = "",
    val instances: List<GarageInstance> = emptyList(),
)

@Serializable
data class GarageInstance(
    val namespace: String = "",
    val name: String = "",
    /** Pod the CLI ran in, "" when none was ready. */
    val pod: String = "",
    val pods: Int = 0,
    val podsReady: Int = 0,
    val version: String = "",
    /** healthy, degraded, unavailable or unknown. */
    val status: String = "",
    /** English, causes first (from the Go core). */
    val message: String = "",
    val connectedNodes: Int = 0,
    val knownNodes: Int = 0,
    val storageNodes: Int = 0,
    val storageNodesUp: Int = 0,
    val partitions: Int = 0,
    val partitionsQuorum: Int = 0,
    val partitionsAllOk: Int = 0,
    /** -1 when unknown. */
    val resyncQueue: Long = -1,
    val resyncErrors: Long = -1,
    val tableSyncQueue: Long = -1,
    val layoutVersion: Long = 0,
    val nodes: List<GarageNode> = emptyList(),
    /** cli-json (full picture) or health (status only). */
    val source: String = "",
) {
    val state: GarageState get() = GarageState.from(status)
    val label: String get() = "$namespace/$name"
    val detailed: Boolean get() = source == "cli-json"
}

@Serializable
data class GarageNode(
    val id: String = "",
    val hostname: String = "",
    val zone: String = "",
    val tags: List<String> = emptyList(),
    /** Kubernetes node of the pod with that hostname, "" when none. */
    val kubeNode: String = "",
    /** Holds a role in the current layout: only those count as down. */
    val storage: Boolean = false,
    val up: Boolean = false,
    /** -1 when up or unknown. */
    val lastSeenSecs: Long = -1,
    val draining: Boolean = false,
    val dataAvail: Long = 0,
    val dataTotal: Long = 0,
    val resyncQueue: Long = -1,
    val resyncErrors: Long = -1,
    val tableSyncQueue: Long = -1,
    val statsError: String = "",
    /** Resync tranquility: 0 resyncs at full speed, 2 is Garage's default, -1 when unknown. */
    val tranquility: Long = -1,
) {
    /** What names the node best: Garage forgets a long-gone node's hostname, its tags often name the host. */
    val label: String get() = hostname.ifEmpty { tags.joinToString(",").ifEmpty { id.take(16) } }
}

@Serializable
data class CnpgStatus(
    val version: String = "",
    val error: String = "",
    val clusters: List<CnpgCluster> = emptyList(),
)

@Serializable
data class CnpgCluster(
    val namespace: String,
    val name: String,
    val phase: String = "",
    val phaseReason: String = "",
    val health: String = "",
    val hibernated: Boolean = false,
    /** Wire values of [CnpgReason]. */
    val reasons: List<String> = emptyList(),
    val instances: Int = 0,
    val readyInstances: Int = 0,
    val currentPrimary: String = "",
    val targetPrimary: String = "",
    val instancePods: List<CnpgPod> = emptyList(),
    /** ok, failing, off or unknown. */
    val archiving: String = "",
    /** ok, failed, stale or none. */
    val lastBackup: String = "",
    /** plugin, in-tree or none. */
    val backupMethod: String = "",
    val objectStore: String = "",
    val scheduled: Boolean = false,
    @SerialName("lastSuccessfulBackupAt") val lastSuccessAt: Long = 0,
    @SerialName("lastFailedBackupAt") val lastFailureAt: Long = 0,
    @SerialName("firstRecoverabilityAt") val recoverableAt: Long = 0,
) {
    val serviceHealth: ServiceHealth get() = ServiceHealth.from(health)
    val label: String get() = "$namespace/$name"
    val reasonList: List<CnpgReason> get() = reasons.mapNotNull(CnpgReason::from)
}

@Serializable
data class CnpgPod(
    val name: String,
    /** "" while Pending: not scheduled anywhere. */
    val node: String = "",
    val phase: String = "",
    /** primary or replica, "" when not running. */
    val role: String = "",
    val ready: Boolean = false,
)

@Serializable
data class DragonflyStatus(
    val version: String = "",
    val error: String = "",
    val instances: List<DragonflyInstance> = emptyList(),
)

@Serializable
data class DragonflyInstance(
    val namespace: String = "",
    val name: String = "",
    /** The operator's own word: Ready, or a step such as a rolling update. */
    val phase: String = "",
    val health: String = "",
    /** Wire values of [DragonflyReason]. */
    val reasons: List<String> = emptyList(),
    val replicas: Int = 0,
    val readyPods: Int = 0,
    /** Pod with role=master, "" when none. */
    val master: String = "",
    /** The master first. */
    val pods: List<DragonflyPod> = emptyList(),
) {
    val serviceHealth: ServiceHealth get() = ServiceHealth.from(health)
    val label: String get() = "$namespace/$name"
    val reasonList: List<DragonflyReason> get() = reasons.mapNotNull(DragonflyReason::from)
}

@Serializable
data class DragonflyPod(
    val name: String,
    val node: String = "",
    val phase: String = "",
    /** The operator's role label: master or replica. */
    val role: String = "",
    val ready: Boolean = false,
)

@Serializable
data class CertManagerStatus(
    val version: String = "",
    val error: String = "",
    /** Worst first, then the soonest expiry. */
    val certificates: List<Certificate> = emptyList(),
    val issuers: List<CertIssuer> = emptyList(),
)

@Serializable
data class Certificate(
    val namespace: String = "",
    val name: String = "",
    val secretName: String = "",
    /** The common name then the DNS names, the first few; [dnsNameCount] counts them all. */
    val dnsNames: List<String> = emptyList(),
    val dnsNameCount: Int = 0,
    /** "ClusterIssuer/letsencrypt", "Issuer/internal-ca". */
    val issuer: String = "",
    val health: String = "",
    /** Wire values of [CertReason]. */
    val reasons: List<String> = emptyList(),
    val ready: Boolean = false,
    /** cert-manager is issuing it now (a renewal, or one forced from the app). */
    val issuing: Boolean = false,
    /** The Ready condition's message when not ready. */
    val message: String = "",
    /** Unix ms, 0 before the first issuance. */
    val notAfter: Long = 0,
    /** Unix ms, 0 when no renewal is planned. */
    val renewalTime: Long = 0,
    val failedAttempts: Int = 0,
) {
    val serviceHealth: ServiceHealth get() = ServiceHealth.from(health)
    val label: String get() = "$namespace/$name"
    val reasonList: List<CertReason> get() = reasons.mapNotNull(CertReason::from)
}

@Serializable
data class CertIssuer(
    /** Issuer or ClusterIssuer. */
    val kind: String = "",
    /** "" for a ClusterIssuer. */
    val namespace: String = "",
    val name: String = "",
    /** acme, ca, selfSigned, vault or venafi; "" for another. */
    val type: String = "",
    /** The ACME server's host. */
    val server: String = "",
    val ready: Boolean = false,
    val message: String = "",
    val health: String = "",
) {
    val serviceHealth: ServiceHealth get() = ServiceHealth.from(health)

    /** "ClusterIssuer/letsencrypt", "Issuer/app/internal-ca": unique across both kinds. */
    val label: String get() = listOf(kind, namespace, name).filter { it.isNotEmpty() }.joinToString("/")
}

/** Why a certificate is not ok, as the Go core names it. */
enum class CertReason(val wire: String) {
    EXPIRED("expired"),
    EXPIRING("expiring"),
    RENEWAL_OVERDUE("renewalOverdue"),
    NOT_READY("notReady"),
    ISSUER("issuer"),
    ;

    companion object {
        fun from(wire: String): CertReason? = entries.firstOrNull { it.wire == wire }
    }
}

@Serializable
data class CephStatus(
    val version: String = "",
    val error: String = "",
    val clusters: List<CephCluster> = emptyList(),
    val pools: List<CephPool> = emptyList(),
    /** By namespace, then OSD number. */
    val osds: List<CephOsd> = emptyList(),
)

@Serializable
data class CephCluster(
    val namespace: String = "",
    val name: String = "",
    /** Rook's own word: Ready (Connected when external), Progressing, Failure... */
    val phase: String = "",
    val message: String = "",
    /** HEALTH_OK, HEALTH_WARN or HEALTH_ERR; "" before Ceph reported. */
    val cephHealth: String = "",
    val health: String = "",
    /** Wire values of [CephReason]. */
    val reasons: List<String> = emptyList(),
    /** Ceph's health checks, errors first. */
    val checks: List<CephCheck> = emptyList(),
    val bytesTotal: Long = 0,
    val bytesUsed: Long = 0,
    val osdsUp: Int = 0,
    val osdsTotal: Int = 0,
    val monsReady: Int = 0,
    val monsTotal: Int = 0,
    /** Nodes of its OSD and mon pods that are not ready. */
    val notReadyNodes: List<String> = emptyList(),
    /** Ceph's version, e.g. 19.2.3-0. */
    val version: String = "",
    val external: Boolean = false,
) {
    val serviceHealth: ServiceHealth get() = ServiceHealth.from(health)
    val label: String get() = "$namespace/$name"
    val reasonList: List<CephReason> get() = reasons.mapNotNull(CephReason::from)

    /** Raw capacity used, 0 when Ceph has not reported it. */
    val usedFraction: Double get() = if (bytesTotal > 0) bytesUsed.toDouble() / bytesTotal else 0.0
}

@Serializable
data class CephCheck(
    /** MON_DOWN, OSD_NEARFULL... */
    val name: String,
    /** HEALTH_WARN or HEALTH_ERR. */
    val severity: String = "",
    val message: String = "",
)

/** A block pool, a filesystem or an object store. */
@Serializable
data class CephPool(
    val namespace: String = "",
    val name: String = "",
    /** blockPool, filesystem or objectStore. */
    val kind: String = "",
    val phase: String = "",
    val health: String = "",
) {
    val serviceHealth: ServiceHealth get() = ServiceHealth.from(health)
    val label: String get() = "$namespace/$name"
}

@Serializable
data class CephOsd(
    /** The cluster's. */
    val namespace: String = "",
    /** The ceph-osd-id label. */
    val id: String = "",
    val pod: String = "",
    val node: String = "",
    val phase: String = "",
    val ready: Boolean = false,
)

/** Why a Ceph cluster is not ok, as the Go core names it. */
enum class CephReason(val wire: String) {
    HEALTH_ERR("healthErr"),
    FAILURE("failure"),
    FULL("full"),
    NO_OSD("noOSD"),
    NO_QUORUM("noQuorum"),
    HEALTH_WARN("healthWarn"),
    NEAR_FULL("nearFull"),
    OSDS("osds"),
    MONS("mons"),
    NOT_READY("notReady"),
    ;

    companion object {
        fun from(wire: String): CephReason? = entries.firstOrNull { it.wire == wire }
    }
}


/** Why a Dragonfly instance is not ok, as the Go core names it. */
enum class DragonflyReason(val wire: String) {
    NO_READY("noReady"),
    NO_MASTER("noMaster"),
    MASTERS("masters"),
    PODS("pods"),
    NOT_READY("notReady"),
    ;

    companion object {
        fun from(wire: String): DragonflyReason? = entries.firstOrNull { it.wire == wire }
    }
}

@Serializable
data class MariaDbStatus(
    val version: String = "",
    val error: String = "",
    val clusters: List<MariaDbCluster> = emptyList(),
)

@Serializable
data class MariaDbCluster(
    val namespace: String = "",
    val name: String = "",
    /** standalone, replication or galera. */
    val topology: String = "",
    val health: String = "",
    /** Wire values of [MariaDbReason]. */
    val reasons: List<String> = emptyList(),
    val suspended: Boolean = false,
    /** The operator's Ready condition message when it is not True. */
    val message: String = "",
    val replicas: Int = 0,
    val readyPods: Int = 0,
    /** The operator's current primary, "" when none. */
    val primary: String = "",
    /** The primary first. */
    val pods: List<MariaDbPod> = emptyList(),
    /** Last successful and failed backup (logical or physical), unix ms, 0 when none. */
    val lastBackupAt: Long = 0,
    val lastBackupFailedAt: Long = 0,
    /** The most frequent active backup cron, "" when none. */
    val backupSchedule: String = "",
) {
    val serviceHealth: ServiceHealth get() = ServiceHealth.from(health)
    val label: String get() = "$namespace/$name"
    val reasonList: List<MariaDbReason> get() = reasons.mapNotNull(MariaDbReason::from)
}

@Serializable
data class MariaDbPod(
    val name: String,
    val node: String = "",
    val phase: String = "",
    /** primary, replica or member (Galera), "" when unknown. */
    val role: String = "",
    val ready: Boolean = false,
)

/** Why a MariaDB cluster is not ok, as the Go core names it. */
enum class MariaDbReason(val wire: String) {
    NO_READY("noReady"),
    NO_PRIMARY("noPrimary"),
    PODS("pods"),
    GALERA_RECOVERY("galeraRecovery"),
    BACKUP_FAILED("backupFailed"),
    BACKUP_STALE("backupStale"),
    NOT_READY("notReady"),
    ;

    companion object {
        fun from(wire: String): MariaDbReason? = entries.firstOrNull { it.wire == wire }
    }
}

@Serializable
data class PerconaStatus(
    val version: String = "",
    val error: String = "",
    val clusters: List<PerconaCluster> = emptyList(),
)

@Serializable
data class PerconaCluster(
    val namespace: String = "",
    val name: String = "",
    /** The operator's own word: ready, initializing, paused, stopping, error or unknown. */
    val state: String = "",
    /** The operator's messages, "; "-joined. */
    val message: String = "",
    val crVersion: String = "",
    val paused: Boolean = false,
    val health: String = "",
    /** Wire values of [PerconaReason]. */
    val reasons: List<String> = emptyList(),
    val pxcSize: Int = 0,
    val pxcReady: Int = 0,
    /** haproxy, proxysql or "" for none. */
    val proxy: String = "",
    val proxySize: Int = 0,
    val proxyReady: Int = 0,
    /** The PXC members, by name. */
    val pods: List<PerconaPod> = emptyList(),
    val lastBackupAt: Long = 0,
    val lastBackupFailedAt: Long = 0,
    val backupSchedules: List<PerconaSchedule> = emptyList(),
) {
    val serviceHealth: ServiceHealth get() = ServiceHealth.from(health)
    val label: String get() = "$namespace/$name"
    val reasonList: List<PerconaReason> get() = reasons.mapNotNull(PerconaReason::from)
}

@Serializable
data class PerconaPod(
    val name: String,
    /** "" while Pending: not scheduled anywhere. */
    val node: String = "",
    val phase: String = "",
    val ready: Boolean = false,
)

@Serializable
data class PerconaSchedule(
    val name: String = "",
    val schedule: String = "",
    val keep: Int = 0,
    val storageName: String = "",
)

/** Why a Percona XtraDB cluster is not ok, as the Go core names it. */
enum class PerconaReason(val wire: String) {
    ERROR("error"),
    NO_MEMBER("noMember"),
    MEMBERS("members"),
    PROXY("proxy"),
    INITIALIZING("initializing"),
    BACKUP_FAILED("backupFailed"),
    BACKUP_STALE("backupStale"),
    ;

    companion object {
        fun from(wire: String): PerconaReason? = entries.firstOrNull { it.wire == wire }
    }
}

@Serializable
data class VeleroStatus(
    val version: String = "",
    val error: String = "",
    /** Problems first. */
    val schedules: List<VeleroSchedule> = emptyList(),
    /** Backups taken by hand (no schedule) that failed in the last week, newest first. */
    val adhoc: List<VeleroAdhocBackup> = emptyList(),
    /** Unavailable first. */
    val locations: List<VeleroLocation> = emptyList(),
)

@Serializable
data class VeleroSchedule(
    val namespace: String = "",
    val name: String = "",
    /** Cron, as written. */
    val schedule: String = "",
    val paused: Boolean = false,
    /** The schedule's own: New, Enabled or FailedValidation. */
    val phase: String = "",
    val validationErrors: List<String> = emptyList(),
    val health: String = "",
    /** Wire values of [VeleroReason]. */
    val reasons: List<String> = emptyList(),
    val storageLocation: String = "",
    /** Empty or "*": every namespace. */
    val includedNamespaces: List<String> = emptyList(),
    /** The latest finished backup, null when none is left. */
    val lastBackup: VeleroBackup? = null,
    /** The latest Completed backup (unix ms), 0 when none. */
    val lastSuccessAt: Long = 0,
    val inProgress: Boolean = false,
) {
    val serviceHealth: ServiceHealth get() = ServiceHealth.from(health)
    val label: String get() = "$namespace/$name"
    val reasonList: List<VeleroReason> get() = reasons.mapNotNull(VeleroReason::from)
}

@Serializable
data class VeleroBackup(
    val name: String = "",
    /** Completed, PartiallyFailed, Failed or FailedValidation. */
    val phase: String = "",
    val startedAt: Long = 0,
    val completedAt: Long = 0,
    val errors: Int = 0,
    val warnings: Int = 0,
    val failureReason: String = "",
)

/** A backup taken by hand that failed: a warning. */
@Serializable
data class VeleroAdhocBackup(
    val namespace: String = "",
    val name: String = "",
    val phase: String = "",
    val startedAt: Long = 0,
    val completedAt: Long = 0,
    val errors: Int = 0,
    val warnings: Int = 0,
    val failureReason: String = "",
    val storageLocation: String = "",
    val health: String = "",
) {
    val serviceHealth: ServiceHealth get() = ServiceHealth.from(health)
    val label: String get() = "$namespace/$name"
}

@Serializable
data class VeleroLocation(
    val namespace: String = "",
    val name: String = "",
    val provider: String = "",
    val bucket: String = "",
    val default: Boolean = false,
    /** Available or Unavailable, "" before the first check. */
    val phase: String = "",
    val message: String = "",
    val lastValidatedAt: Long = 0,
    val health: String = "",
) {
    val serviceHealth: ServiceHealth get() = ServiceHealth.from(health)
    val label: String get() = "$namespace/$name"
}

/** Why a Velero schedule is not ok, as the Go core names it. */
enum class VeleroReason(val wire: String) {
    FAILED("failed"),
    LOCATION("location"),
    PARTIALLY_FAILED("partiallyFailed"),
    STALE("stale"),
    INVALID("invalid"),
    ;

    companion object {
        fun from(wire: String): VeleroReason? = entries.firstOrNull { it.wire == wire }
    }
}

/** Health of one item (a volume, a Postgres cluster), worst first. */
enum class ServiceHealth(val wire: String) {
    CRITICAL("critical"),
    WARNING("warning"),
    UNKNOWN(""),
    OK("ok"),
    IDLE("idle"),
    ;

    val needsAttention: Boolean get() = this == CRITICAL || this == WARNING

    companion object {
        fun from(wire: String): ServiceHealth = entries.firstOrNull { it.wire == wire && wire.isNotEmpty() } ?: UNKNOWN

        /** The worst of [healths], [OK] when there is none. */
        fun worst(healths: Iterable<ServiceHealth>): ServiceHealth = healths.minByOrNull { it.ordinal }?.takeIf { it != IDLE } ?: OK
    }
}

enum class GarageState(val wire: String, val health: ServiceHealth) {
    HEALTHY("healthy", ServiceHealth.OK),
    DEGRADED("degraded", ServiceHealth.WARNING),
    UNAVAILABLE("unavailable", ServiceHealth.CRITICAL),
    UNKNOWN("unknown", ServiceHealth.UNKNOWN),
    ;

    companion object {
        fun from(wire: String): GarageState = entries.firstOrNull { it.wire == wire } ?: UNKNOWN
    }
}

/** Why a Postgres cluster is not ok, as the Go core names it. */
enum class CnpgReason(val wire: String) {
    NO_INSTANCE("noInstance"),
    FAILOVER("failover"),
    INSTANCES("instances"),
    SWITCHOVER("switchover"),
    NOT_READY("notReady"),
    ARCHIVING("archiving"),
    BACKUP_FAILED("backupFailed"),
    BACKUP_STALE("backupStale"),
    ;

    companion object {
        fun from(wire: String): CnpgReason? = entries.firstOrNull { it.wire == wire }
    }
}

/** The systems, in the order the app shows them, with their app catalog id. */
enum class DataServiceKind(val catalogId: String) {
    LONGHORN("longhorn"),
    GARAGE("garage"),
    CNPG("cloudnative-pg"),
    DRAGONFLY("dragonfly"),
    MARIADB("mariadb"),
    PERCONA("percona-xtradb"),
    CERT_MANAGER("cert-manager"),
    VELERO("velero"),
    CEPH("rook"),
}

/** Product names: never translated. */
val DataServiceKind.title: String
    get() = when (this) {
        DataServiceKind.LONGHORN -> "Longhorn"
        DataServiceKind.GARAGE -> "Garage"
        DataServiceKind.CNPG -> "CloudNativePG"
        DataServiceKind.DRAGONFLY -> "Dragonfly"
        DataServiceKind.MARIADB -> "MariaDB"
        DataServiceKind.PERCONA -> "Percona XtraDB Cluster"
        DataServiceKind.CERT_MANAGER -> "cert-manager"
        DataServiceKind.VELERO -> "Velero"
        DataServiceKind.CEPH -> "Rook Ceph"
    }

/** The system a data alert's "system|label" key names (monitor dataIssuesOf). */
val DataServiceKind.alertSystem: String
    get() = when (this) {
        DataServiceKind.LONGHORN -> "longhorn"
        DataServiceKind.GARAGE -> "garage"
        DataServiceKind.CNPG -> "cnpg"
        DataServiceKind.DRAGONFLY -> "dragonfly"
        DataServiceKind.MARIADB -> "mariadb"
        DataServiceKind.PERCONA -> "percona"
        DataServiceKind.CERT_MANAGER -> "certmanager"
        DataServiceKind.VELERO -> "velero"
        DataServiceKind.CEPH -> "ceph"
    }

/** The kind behind a data alert's system name; CloudNativePG for one an older snapshot wrote. */
fun dataServiceKindOf(alertSystem: String): DataServiceKind =
    DataServiceKind.entries.firstOrNull { it.alertSystem == alertSystem } ?: DataServiceKind.CNPG

/** The catalog ids among the inventory's apps, for KubeDataServices: "" when none runs. */
fun Inventory.dataServiceHints(): String {
    val ids = apps.map { it.id }.toSet()
    return DataServiceKind.entries.map { it.catalogId }.filter { it in ids }.joinToString(",")
}

/** The installed systems, in display order. */
val DataServices.detected: List<DataServiceKind>
    get() = listOfNotNull(
        DataServiceKind.LONGHORN.takeIf { longhorn != null },
        DataServiceKind.GARAGE.takeIf { garage != null },
        DataServiceKind.CNPG.takeIf { cnpg != null },
        DataServiceKind.DRAGONFLY.takeIf { dragonfly != null },
        DataServiceKind.MARIADB.takeIf { mariadb != null },
        DataServiceKind.PERCONA.takeIf { percona != null },
        DataServiceKind.CERT_MANAGER.takeIf { certManager != null },
        DataServiceKind.VELERO.takeIf { velero != null },
        DataServiceKind.CEPH.takeIf { ceph != null },
    )

/**
 * One system at a glance: [total] items (volumes, Garage clusters, Postgres clusters), how many
 * need attention, its worst health, and the error when it could not be read.
 */
data class ServiceSummary(val total: Int, val attention: Int, val health: ServiceHealth, val error: String = "")

fun DataServices.summary(kind: DataServiceKind): ServiceSummary? = when (kind) {
    DataServiceKind.LONGHORN -> longhorn?.summary()
    DataServiceKind.GARAGE -> garage?.summary()
    DataServiceKind.CNPG -> cnpg?.summary()
    DataServiceKind.DRAGONFLY -> dragonfly?.summary()
    DataServiceKind.MARIADB -> mariadb?.summary()
    DataServiceKind.PERCONA -> percona?.summary()
    DataServiceKind.CERT_MANAGER -> certManager?.summary()
    DataServiceKind.VELERO -> velero?.summary()
    DataServiceKind.CEPH -> ceph?.summary()
}

/** Ceph clusters are the items; a pool that is not ready needs a look as much as a cluster. */
fun CephStatus.summary(): ServiceSummary {
    if (error.isNotEmpty() && clusters.isEmpty()) return ServiceSummary(0, 0, ServiceHealth.UNKNOWN, error)
    val healths = clusters.map { it.serviceHealth } + pools.map { it.serviceHealth }
    return ServiceSummary(clusters.size, healths.count { it.needsAttention }, ServiceHealth.worst(healths), error)
}

fun MariaDbStatus.summary(): ServiceSummary {
    if (error.isNotEmpty() && clusters.isEmpty()) return ServiceSummary(0, 0, ServiceHealth.UNKNOWN, error)
    val healths = clusters.map { it.serviceHealth }
    return ServiceSummary(clusters.size, healths.count { it.needsAttention }, ServiceHealth.worst(healths), error)
}

fun PerconaStatus.summary(): ServiceSummary {
    if (error.isNotEmpty() && clusters.isEmpty()) return ServiceSummary(0, 0, ServiceHealth.UNKNOWN, error)
    val healths = clusters.map { it.serviceHealth }
    return ServiceSummary(clusters.size, healths.count { it.needsAttention }, ServiceHealth.worst(healths), error)
}

fun CertManagerStatus.summary(): ServiceSummary {
    if (error.isNotEmpty() && certificates.isEmpty()) return ServiceSummary(0, 0, ServiceHealth.UNKNOWN, error)
    // An issuer that is not ready needs a look as much as a certificate.
    val healths = certificates.map { it.serviceHealth } + issuers.map { it.serviceHealth }
    return ServiceSummary(certificates.size, healths.count { it.needsAttention }, ServiceHealth.worst(healths), error)
}

/** [ServiceSummary.total] counts the schedules; a storage location down or a failed backup taken by hand needs a look too. */
fun VeleroStatus.summary(): ServiceSummary {
    if (error.isNotEmpty() && schedules.isEmpty() && locations.isEmpty()) return ServiceSummary(0, 0, ServiceHealth.UNKNOWN, error)
    val healths = schedules.map { it.serviceHealth } + locations.map { it.serviceHealth } + adhoc.map { it.serviceHealth }
    return ServiceSummary(schedules.size, healths.count { it.needsAttention }, ServiceHealth.worst(healths), error)
}

fun DragonflyStatus.summary(): ServiceSummary {
    if (error.isNotEmpty() && instances.isEmpty()) return ServiceSummary(0, 0, ServiceHealth.UNKNOWN, error)
    val healths = instances.map { it.serviceHealth }
    return ServiceSummary(instances.size, healths.count { it.needsAttention }, ServiceHealth.worst(healths), error)
}

fun LonghornStatus.summary(): ServiceSummary {
    if (error.isNotEmpty()) return ServiceSummary(volumes.size, 0, ServiceHealth.UNKNOWN, error)
    // A node that is not ready or a backup target that is gone needs a look as much as a volume.
    val healths = volumes.map { it.serviceHealth } +
        nodes.filter { !it.ready }.map { ServiceHealth.WARNING } +
        backupTargets.filter { !it.available }.map { ServiceHealth.WARNING }
    return ServiceSummary(volumes.size, healths.count { it.needsAttention }, ServiceHealth.worst(healths))
}

fun GarageStatus.summary(): ServiceSummary {
    if (error.isNotEmpty()) return ServiceSummary(instances.size, 0, ServiceHealth.UNKNOWN, error)
    val healths = instances.map { it.state.health }
    return ServiceSummary(instances.size, healths.count { it.needsAttention }, ServiceHealth.worst(healths))
}

fun CnpgStatus.summary(): ServiceSummary {
    if (error.isNotEmpty() && clusters.isEmpty()) return ServiceSummary(0, 0, ServiceHealth.UNKNOWN, error)
    val healths = clusters.map { it.serviceHealth }
    return ServiceSummary(clusters.size, healths.count { it.needsAttention }, ServiceHealth.worst(healths), error)
}

/** Worst health over every installed system. */
val DataServices.worst: ServiceHealth
    get() = ServiceHealth.worst(detected.mapNotNull { summary(it)?.health })

/** A node that is not ready and the problems it most likely explains. */
data class LikelyCause(val node: String, val problems: Int)

/**
 * Nodes that are not ready ([downNodes], plus the ones Longhorn reports) and how many problems
 * each explains: a volume with a replica there, a Garage cluster whose missing node runs there,
 * a Postgres cluster with an instance there that is not ready. Worst first; empty when no
 * problem points to a node.
 */
fun DataServices.likelyCauses(downNodes: Set<String> = emptySet()): List<LikelyCause> {
    val down = downNodes + longhorn?.nodes.orEmpty().filter { !it.ready }.map { it.name }
    if (down.isEmpty()) return emptyList()

    val hits = mutableMapOf<String, Int>()
    fun count(nodes: Collection<String>) = nodes.filter { it in down }.distinct().forEach { hits[it] = (hits[it] ?: 0) + 1 }

    longhorn?.volumes.orEmpty().filter { it.serviceHealth.needsAttention }.forEach { count(it.replicaNodes) }
    garage?.instances.orEmpty().filter { it.state.health.needsAttention }.forEach { inst ->
        count(inst.nodes.filter { !it.up && it.storage }.flatMap { listOf(it.kubeNode) + it.tags })
    }
    cnpg?.clusters.orEmpty().filter { it.serviceHealth.needsAttention }.forEach { c ->
        count(c.instancePods.filter { !it.ready }.map { it.node })
    }
    dragonfly?.instances.orEmpty().filter { it.serviceHealth.needsAttention }.forEach { d ->
        count(d.pods.filter { !it.ready }.map { it.node })
    }
    mariadb?.clusters.orEmpty().filter { it.serviceHealth.needsAttention }.forEach { m ->
        count(m.pods.filter { !it.ready }.map { it.node })
    }
    percona?.clusters.orEmpty().filter { it.serviceHealth.needsAttention }.forEach { c ->
        count(c.pods.filter { !it.ready }.map { it.node })
    }
    ceph?.clusters.orEmpty().filter { it.serviceHealth.needsAttention }.forEach { count(it.notReadyNodes) }

    return hits.map { (node, n) -> LikelyCause(node, n) }.sortedWith(compareByDescending<LikelyCause> { it.problems }.thenBy { it.node })
}

/** Postgres instances waiting unscheduled (no node): often their volume is pinned to a node that is gone. */
val CnpgStatus.pendingInstances: Int
    get() = clusters.sumOf { c -> c.instancePods.count { it.node.isEmpty() && it.phase == "Pending" } }

/** Volumes of [filter], worst first as the Go core sorts them. */
fun List<LonghornVolume>.filtered(filter: VolumeFilter, query: String): List<LonghornVolume> {
    val q = query.trim()
    return filter { v ->
        when (filter) {
            VolumeFilter.ALL -> true
            VolumeFilter.PROBLEMS -> v.serviceHealth.needsAttention
            VolumeFilter.DETACHED -> v.state == "detached"
        } && (q.isEmpty() || v.label.contains(q, ignoreCase = true) || v.name.contains(q, ignoreCase = true))
    }
}

enum class VolumeFilter { ALL, PROBLEMS, DETACHED }

/** Clusters matching [query] by namespace/name, only those needing attention when [problemsOnly]. */
fun List<CnpgCluster>.filtered(problemsOnly: Boolean, query: String): List<CnpgCluster> {
    val q = query.trim()
    return filter { c ->
        (!problemsOnly || c.serviceHealth.needsAttention) && (q.isEmpty() || c.label.contains(q, ignoreCase = true))
    }
}
