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
    val castai: CastAIStatus? = null,
)

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
    CASTAI("castai"),
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
        DataServiceKind.CASTAI -> "CAST AI"
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
        DataServiceKind.CASTAI -> "castai"
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
        DataServiceKind.CASTAI.takeIf { castai != null },
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
    DataServiceKind.CASTAI -> castai?.summary()
}

/** Recommendations are the items; one the autoscaler cannot apply or is told not to needs a look. */
fun CastAIStatus.summary(): ServiceSummary {
    if (error.isNotEmpty() && recommendations.isEmpty()) return ServiceSummary(0, 0, ServiceHealth.UNKNOWN, error)
    val healths = recommendations.map { it.serviceHealth }
    return ServiceSummary(recommendations.size, healths.count { it.needsAttention }, ServiceHealth.worst(healths), error)
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
