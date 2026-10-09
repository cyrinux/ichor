package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo (see DataServices.kt).

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
