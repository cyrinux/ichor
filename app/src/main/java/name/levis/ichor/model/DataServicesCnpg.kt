package name.levis.ichor.model

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

// Mirrors go/ichorgo (see DataServices.kt); the wire format is documented in the Linear plan document "Data services: Longhorn, Garage and CloudNativePG health".

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
