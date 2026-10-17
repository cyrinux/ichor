package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/kube_helm.go: Helm releases read from their Secrets, read-only.

@Serializable
data class HelmRelease(
    val name: String = "",
    val namespace: String = "",
    val revision: Int = 0,
    val status: String = "",
    val chart: String = "",
    val chartVersion: String = "",
    val appVersion: String = "",
    /** Unix seconds of the last deployment. */
    val updated: Long = 0,
) {
    val key: String get() = "$namespace/$name"
}

@Serializable
data class HelmReleaseList(val releases: List<HelmRelease> = emptyList())

@Serializable
data class HelmRevision(
    val revision: Int = 0,
    val status: String = "",
    /** Unix seconds. */
    val updated: Long = 0,
    val description: String = "",
)

/** One release's latest revision in full, and its history. */
@Serializable
data class HelmReleaseDetail(
    val name: String = "",
    val namespace: String = "",
    val revision: Int = 0,
    val status: String = "",
    val chart: String = "",
    val chartVersion: String = "",
    val appVersion: String = "",
    val updated: Long = 0,
    val description: String = "",
    val notes: String = "",
    /** The values the user set, as YAML. */
    val values: String = "",
    val manifest: String = "",
    val history: List<HelmRevision> = emptyList(),
)

/** One object a rollback would touch, checked by a server-side dry run. */
@Serializable
data class HelmRollbackChange(
    /** create, update, delete or keep (a delete skipped by helm.sh/resource-policy: keep). */
    val action: String = "",
    val kind: String = "",
    val namespace: String = "",
    val name: String = "",
    /** Why the API server refused the dry run; empty when it passed. */
    val error: String = "",
)

/** What rolling a release back from revision [from] to [to] would do; nothing changed yet. */
@Serializable
data class HelmRollbackPlan(
    val namespace: String = "",
    val name: String = "",
    val from: Int = 0,
    val to: Int = 0,
    val fromChart: String = "",
    val toChart: String = "",
    val fromAppVersion: String = "",
    val toAppVersion: String = "",
    val changes: List<HelmRollbackChange> = emptyList(),
    /** Objects already as [to] has them, left alone. */
    val unchanged: Int = 0,
    /** "namespace/name" of the Flux HelmRelease managing the release, or empty. */
    val fluxOwner: String = "",
    /** Why it cannot run; empty when it can. */
    val blockers: List<String> = emptyList(),
    /** Updates are planned as whole-object replaces (helm rollback --force). */
    val force: Boolean = false,
    /** Revisions kept after the rollback; 0 keeps them all. */
    val historyMax: Int = 0,
    /** Revisions whose record the rollback deletes once it succeeded, oldest first. */
    val prune: List<Int> = emptyList(),
    /** Deployments, StatefulSets and DaemonSets of the target revision: what a wait follows. */
    val workloads: Int = 0,
)

val HelmRollbackPlan.canRun: Boolean get() = blockers.isEmpty()

/** Helm's default --history-max, and the stepper's upper bound. */
const val HELM_HISTORY_MAX_DEFAULT = 10
const val HELM_HISTORY_MAX_LIMIT = 50

/**
 * How to roll back: [force] and [historyMax] go to the Go core (and change the dry run),
 * [wait] stays in the app (follow the workloads afterwards).
 */
@Serializable
data class HelmRollbackOptions(
    val force: Boolean = false,
    val historyMax: Int = HELM_HISTORY_MAX_DEFAULT,
    val wait: Boolean = true,
) {
    /** Whether going from this to [other] needs a new dry run. */
    fun changesPlan(other: HelmRollbackOptions): Boolean = force != other.force || historyMax != other.historyMax

    fun withHistoryMax(n: Int): HelmRollbackOptions = copy(historyMax = n.coerceIn(0, HELM_HISTORY_MAX_LIMIT))
}

/** What the Go core reads: both fields always present, `wait` left out. */
@Serializable
internal data class HelmRollbackOptionsPayload(val force: Boolean, val historyMax: Int)

internal val HelmRollbackOptions.payload: HelmRollbackOptionsPayload
    get() = HelmRollbackOptionsPayload(force, historyMax.coerceIn(0, HELM_HISTORY_MAX_LIMIT))

/** One Deployment, StatefulSet or DaemonSet followed after a rollback. */
@Serializable
data class HelmRollbackWorkload(
    val kind: String = "",
    val namespace: String = "",
    val name: String = "",
    val ready: Int = 0,
    val desired: Int = 0,
    val done: Boolean = false,
    /** Its progress deadline was exceeded. */
    val failed: Boolean = false,
    /** OnDelete or a StatefulSet partition: counted as done, its pods change only when deleted. */
    val manual: Boolean = false,
) {
    val label: String get() = "$kind $name"
}

object HelmRollbackPhase {
    const val RUNNING = "Running"
    const val SUCCEEDED = "Succeeded"
    const val FAILED = "Failed"
    const val TIMEOUT = "Timeout"
}

/** A step of StartHelmRollbackFollow, sent on change only. */
@Serializable
data class HelmRollbackFollowProgress(
    /** Running, Succeeded, Failed or Timeout. */
    val phase: String = "",
    val done: Int = 0,
    val total: Int = 0,
    val workloads: List<HelmRollbackWorkload> = emptyList(),
    /** Unix millis. */
    val at: Long = 0,
) {
    /** done/total in 0..1; 1 when there is nothing to follow. */
    val fraction: Float get() = if (total <= 0) 1f else (done.toFloat() / total).coerceIn(0f, 1f)

    /** The first workload that failed, for the outcome line. */
    val firstFailed: HelmRollbackWorkload? get() = workloads.firstOrNull { it.failed }
}

/** How a Helm status reads at a glance: deployed fine, failed bad, pending-* in progress. */
fun helmStatusTone(status: String): CellTone = when {
    status == "deployed" -> CellTone.OK
    status == "failed" -> CellTone.BAD
    status.startsWith("pending") || status == "uninstalling" -> CellTone.WARN
    else -> CellTone.NONE
}

/** [releases] whose name, namespace or chart contains [query] (case-insensitive). */
fun List<HelmRelease>.filteredReleases(query: String): List<HelmRelease> {
    val q = query.trim()
    if (q.isEmpty()) return this
    return filter { it.name.contains(q, true) || it.namespace.contains(q, true) || it.chart.contains(q, true) }
}

/**
 * Whether a KubeHelmRollback error only says the history was not pruned afterwards
 * ("history not pruned: …"): the rollback itself is done.
 */
fun isHelmHistoryNotPruned(message: String?): Boolean = message?.contains(HELM_HISTORY_NOT_PRUNED) == true

private const val HELM_HISTORY_NOT_PRUNED = "history not pruned:"
