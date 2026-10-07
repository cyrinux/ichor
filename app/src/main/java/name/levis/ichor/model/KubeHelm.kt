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
