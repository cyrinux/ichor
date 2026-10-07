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
)

val HelmRollbackPlan.canRun: Boolean get() = blockers.isEmpty()

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
