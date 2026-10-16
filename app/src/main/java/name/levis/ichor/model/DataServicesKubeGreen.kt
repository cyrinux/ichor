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

/**
 * What kube-green has done to one namespace, read when a workload or CronJob row is opened so
 * it can say the namespace is asleep before a scale, a restart or a resume. Mirrors the JSON of
 * `go/ichorgo/kube_kubegreen_namespace.go`; never cached on disk.
 */
@Serializable
data class KubeGreenNamespace(
    /** False when kube-green is not installed, or the list was refused: nothing to say. */
    val installed: Boolean = false,
    val asleep: Boolean = false,
    val sleepInfos: List<KubeGreenSleepInfo> = emptyList(),
    val sleepsDeployments: Boolean = false,
    val sleepsStatefulSets: Boolean = false,
    val sleepsCronJobs: Boolean = false,
    /** Unix ms, 0 when none. */
    val nextWakeUpAt: Long = 0,
    /** Unix ms, 0 when none. */
    val nextSleepAt: Long = 0,
    /** A SleepInfo has excludeRef or includeRef: which objects it touches cannot be told, say "may be". */
    val filtered: Boolean = false,
) {
    /** The SleepInfo that has the namespace asleep now; null when none does. */
    val asleepInfo: KubeGreenSleepInfo? get() = sleepInfos.firstOrNull { it.asleep }

    /** Whether kube-green sleeps the [kind] this row is ("Deployment", "StatefulSet", "CronJob"). */
    fun sleeps(kind: String): Boolean = when (kind) {
        "Deployment" -> sleepsDeployments
        "StatefulSet" -> sleepsStatefulSets
        "CronJob" -> sleepsCronJobs
        else -> false
    }

    /**
     * Whether a change made now is undone by kube-green: [KubeGreenWarn.ASLEEP] while it sleeps
     * the namespace, [KubeGreenWarn.SLEEPS_SOON] when it sleeps it within a day, else null (it
     * will not touch the change, or only much later).
     */
    fun warnBeforeChange(now: Long = System.currentTimeMillis()): KubeGreenWarn? = when {
        asleep -> KubeGreenWarn.ASLEEP
        nextSleepAt in 1L..(now + KUBE_GREEN_SOON_MILLIS) -> KubeGreenWarn.SLEEPS_SOON
        else -> null
    }
}

/** Why kube-green would undo a change made by hand: the namespace is asleep, or sleeps soon. */
enum class KubeGreenWarn { ASLEEP, SLEEPS_SOON }

/** A day, the window in which the Go core also warns: see `kubeGreenWarningText`. */
private const val KUBE_GREEN_SOON_MILLIS = 24 * 60 * 60 * 1000L

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
