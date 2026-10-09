package name.levis.ichor.model

import kotlinx.serialization.Serializable

/** The Jobs screen (Go `KubeJobs`): each Job, failures first, then suspended and running ones. */
@Serializable
data class KubeJobs(val jobs: List<JobRow> = emptyList())

/**
 * A Job. [state]: running, succeeded, failed or suspended. [started] and [finished] in unix
 * millis ([finished] 0 while running); [duration] in millis, up to the read while it runs.
 * [completions]: kubectl's "1/1". [owner]: the CronJob that created it, "" for none.
 */
@Serializable
data class JobRow(
    val namespace: String = "",
    val name: String = "",
    val state: String = "",
    val owner: String = "",
    val manual: Boolean = false,
    val started: Long = 0,
    val finished: Long = 0,
    val duration: Long = 0,
    val completions: String = "",
    /** Why a failed Job stopped (BackoffLimitExceeded, DeadlineExceeded...). */
    val reason: String = "",
    /** Read as a [StorageLevel]: the screens share ok, warning and critical. */
    @Serializable(with = StorageLevelSerializer::class)
    val level: StorageLevel = StorageLevel.OK,
) {
    val key: String get() = "$namespace/$name"

    /** Held by spec.suspend: it runs no pod until resumed. */
    val suspended: Boolean get() = state == SUSPENDED

    /** The run state the CronJobs screen also shows; null for a suspended Job. */
    val runState: JobRunState? get() = if (suspended) null else JobRunState.from(state)

    /** How long it ran, in seconds; null before it started. */
    val durationSeconds: Long? get() = (duration / 1000).takeIf { started > 0 && duration > 0 }

    val ref: KubeObjectRef get() = KubeObjectRef.job(namespace, name)

    /** The CronJob that created it, null for a Job of its own. */
    val ownerRef: KubeObjectRef? get() = owner.takeIf { it.isNotEmpty() }?.let { KubeObjectRef.cronJob(namespace, it) }

    companion object {
        const val SUSPENDED = "suspended"
    }
}

/** The Jobs whose namespace/name, owner CronJob, state or reason contains [query] (any case). */
fun List<JobRow>.filteredJobs(query: String): List<JobRow> {
    val q = query.trim()
    if (q.isEmpty()) return this
    return filter { j -> listOf(j.key, j.owner, j.state, j.reason).any { it.contains(q, ignoreCase = true) } }
}
