package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo (see DataServices.kt); the wire format is documented in plans/data-services/README.md.

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
