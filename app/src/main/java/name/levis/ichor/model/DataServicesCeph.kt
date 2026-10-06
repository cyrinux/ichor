package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo (see DataServices.kt); the wire format is documented in plans/data-services/README.md.

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
