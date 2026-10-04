package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/kube_cronjobs.go.

@Serializable
data class KubeCronJobList(val cronJobs: List<KubeCronJob> = emptyList())

@Serializable
data class KubeCronJob(
    val namespace: String,
    val name: String,
    /** ichor.levis.name/title: a display name; "" when not set. */
    val title: String = "",
    /** ichor.levis.name/description: one line on what it does; "" when not set. */
    val description: String = "",
    /** Bundled icon (assets/appicons); "" when none. */
    val icon: String = "",
    /** Dashboard Icons slug, downloaded only when the user allowed it; "" when none. */
    val remoteIcon: String = "",
    val schedule: String = "",
    val timeZone: String = "",
    val suspended: Boolean = false,
    /** False when ichor.levis.name/trigger=false: runs on schedule only. */
    val triggerable: Boolean = true,
    val active: Int = 0,
    /** The latest run's: running, succeeded, failed or never. */
    val state: String = "",
    /** Unix millis, 0 when never. */
    val lastSchedule: Long = 0,
    val lastSuccess: Long = 0,
    /** Unix millis, 0 when suspended or unknown. */
    val nextRun: Long = 0,
    val created: Long = 0,
    val images: List<String> = emptyList(),
    /** Newest first. */
    val runs: List<KubeJobRun> = emptyList(),
) {
    val key: String get() = "$namespace/$name"

    /** The title when set, else the CronJob's name. */
    val displayName: String get() = title.ifBlank { name }

    val runState: JobRunState get() = JobRunState.from(state)

    /** The icon tile's app: the resolved icon, a default CronJob glyph when there is none. */
    val iconApp: InventoryApp get() = InventoryApp(id = "cronjob:$key", name = displayName, icon = icon, remoteIcon = remoteIcon)

    val hasIcon: Boolean get() = icon.isNotEmpty() || iconApp.remoteIconSlug != null
}

@Serializable
data class KubeJobRun(
    val name: String,
    val state: String = "",
    /** Started by hand (Ichor, kubectl create job --from). */
    val manual: Boolean = false,
    /** Unix millis. */
    val started: Long = 0,
    /** Unix millis, 0 while running. */
    val finished: Long = 0,
) {
    val runState: JobRunState get() = JobRunState.from(state)

    /** How long it ran, null while it runs or when unknown. */
    val durationMillis: Long? get() = (finished - started).takeIf { finished > 0 && started > 0 && it >= 0 }
}

enum class JobRunState(val wire: String) {
    RUNNING("running"),
    SUCCEEDED("succeeded"),
    FAILED("failed"),
    NEVER("never"),
    ;

    companion object {
        fun from(wire: String): JobRunState = entries.firstOrNull { it.wire == wire } ?: NEVER
    }
}

/** Namespaces that have CronJobs, sorted. */
val List<KubeCronJob>.cronNamespaces: List<String>
    get() = map { it.namespace }.distinct().sorted()

/**
 * CronJobs of [namespace] (all when null) whose name, title, description, schedule or image
 * contains [query] (case-insensitive): running ones first, then failed, then by namespace
 * and name.
 */
fun List<KubeCronJob>.filteredCronJobs(namespace: String?, query: String): List<KubeCronJob> {
    val q = query.trim()
    return filter { c ->
        (namespace == null || c.namespace == namespace) &&
            (q.isEmpty() || listOf(c.name, c.title, c.description, c.schedule).any { it.contains(q, ignoreCase = true) } ||
                c.images.any { it.contains(q, ignoreCase = true) })
    }.sortedWith(compareBy<KubeCronJob> { it.attentionRank }.thenBy { it.namespace }.thenBy { it.name })
}

private val KubeCronJob.attentionRank: Int
    get() = when (runState) {
        JobRunState.RUNNING -> 0
        JobRunState.FAILED -> 1
        else -> 2
    }
