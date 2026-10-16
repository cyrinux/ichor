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

// The schedule editor's values, checked the way the SleepInfo CRD and the Go core check them,
// so a bad one is refused in the sheet before any request.

private val KUBE_GREEN_TIME = Regex("^[0-9*,/-]+:[0-9*,/-]+$")
private val KUBE_GREEN_WEEKDAYS = Regex("^[0-6*,/-]+$")

/**
 * The days a cron day-of-week field picks (0 Sunday … 6 Saturday; 7 is Sunday too); null when it
 * uses steps or anything else day chips cannot show, which is then edited as text.
 */
fun kubeGreenDays(weekdays: String): Set<Int>? {
    val w = weekdays.trim()
    if (w.isEmpty() || w == "*") return (0..6).toSet()
    val days = mutableSetOf<Int>()
    for (part in w.split(",")) {
        val bounds = part.split("-").map { it.trim().toIntOrNull()?.takeIf { d -> d in 0..7 } ?: return null }
        when (bounds.size) {
            1 -> days += bounds[0] % 7
            2 -> {
                if (bounds[0] > bounds[1]) return null
                for (d in bounds[0]..bounds[1]) days += d % 7
            }
            else -> return null
        }
    }
    return days
}

/** The cron day-of-week field for [days]: "*" for every day, else runs of days ("1-5", "0,6"). */
fun kubeGreenWeekdays(days: Set<Int>): String {
    if (days.containsAll((0..6).toList())) return "*"
    val runs = mutableListOf<IntRange>()
    for (d in days.filter { it in 0..6 }.sorted()) {
        val last = runs.lastOrNull()
        if (last != null && last.last + 1 == d) runs[runs.size - 1] = last.first..d else runs += d..d
    }
    return runs.joinToString(",") { if (it.first == it.last) "${it.first}" else "${it.first}-${it.last}" }
}

/** A sleepAt or wakeUpAt kube-green takes: "HH:mm", each part cron syntax; plain numbers make a real time. */
fun kubeGreenTimeValid(at: String): Boolean {
    val t = at.trim()
    if (!KUBE_GREEN_TIME.matches(t)) return false
    val (hour, minute) = t.split(":")
    if ((hour.toIntOrNull() ?: 0) > 23) return false
    return (minute.toIntOrNull() ?: 0) <= 59
}

fun kubeGreenWeekdaysValid(weekdays: String): Boolean = KUBE_GREEN_WEEKDAYS.matches(weekdays.trim())

/** An IANA zone the phone knows ("" is UTC). */
fun kubeGreenZoneValid(zone: String): Boolean =
    zone.isBlank() || runCatching { java.time.ZoneId.of(zone.trim()) }.isSuccess
