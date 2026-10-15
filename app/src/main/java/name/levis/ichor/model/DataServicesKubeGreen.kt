package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/kube_kubegreen.go.

@Serializable
data class KubeGreenStatus(
    val version: String = "",
    val error: String = "",
    /** Problems first. */
    val sleepInfos: List<KubeGreenSleepInfo> = emptyList(),
)

@Serializable
data class KubeGreenSleepInfo(
    val namespace: String = "",
    val name: String = "",
    /** Cron day-of-week, as written ("1-5"). */
    val weekdays: String = "",
    /** "HH:mm", each part cron syntax. */
    val sleepAt: String = "",
    /** Empty: it sleeps until changed by hand. */
    val wakeUpAt: String = "",
    /** IANA, empty is UTC. */
    val timeZone: String = "",
    val suspendDeployments: Boolean = true,
    val suspendStatefulSets: Boolean = true,
    val suspendCronJobs: Boolean = false,
    val excludeCount: Int = 0,
    val includeCount: Int = 0,
    val patchCount: Int = 0,
    /** What the controller did last: SLEEP, WAKE_UP or "" before the first run. */
    val operation: String = "",
    /** Unix ms, 0 when none. */
    val lastScheduleAt: Long = 0,
    /** Unix ms, 0 when the schedule does not parse. */
    val nextSleepAt: Long = 0,
    /** Unix ms, 0 when [wakeUpAt] is empty. */
    val nextWakeUpAt: Long = 0,
    val asleep: Boolean = false,
    val health: String = "",
    val reasons: List<String> = emptyList(),
) {
    val serviceHealth: ServiceHealth get() = ServiceHealth.from(health)
    val label: String get() = "$namespace/$name"
    val reasonList: List<KubeGreenReason> get() = reasons.mapNotNull(KubeGreenReason::from)
}

/** Why a SleepInfo is not ok, as the Go core names it. */
enum class KubeGreenReason(val wire: String) {
    NO_WAKE_UP("noWakeUp"),
    INVALID_SCHEDULE("invalidSchedule"),
    OVERDUE("overdue"),
    NEVER_RAN("neverRan"),
    ;

    companion object {
        fun from(wire: String): KubeGreenReason? = entries.firstOrNull { it.wire == wire }
    }
}

/** Sleep schedules are the items; one that needs a look is a warning, never critical. */
fun KubeGreenStatus.summary(): ServiceSummary {
    if (error.isNotEmpty() && sleepInfos.isEmpty()) return ServiceSummary(0, 0, ServiceHealth.UNKNOWN, error)
    val healths = sleepInfos.map { it.serviceHealth }
    return ServiceSummary(sleepInfos.size, healths.count { it.needsAttention }, ServiceHealth.worst(healths), error)
}
