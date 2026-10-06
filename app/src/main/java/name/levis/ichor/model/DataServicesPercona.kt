package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo (see DataServices.kt); the wire format is documented in plans/data-services/README.md.

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
