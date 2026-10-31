package name.levis.ichor.monitor

import name.levis.ichor.model.KubeSpanMeshHealth

const val KUBESPAN_REASON_NEVER = "neverHandshaked"
const val KUBESPAN_REASON_STALE = "staleHandshake"
const val KUBESPAN_REASON_NO_ENDPOINT = "noEndpoint"
const val KUBESPAN_REASON_ONE_SIDE = "oneSide"

/**
 * A KubeSpan issue's stored value as Go writes it, split: "severity|reason|since|host-a|host-b"
 * ([since] unix seconds of the last handshake, 0 = never).
 */
data class KubeSpanDetail(
    val severity: String,
    val reason: String,
    val since: Long,
    val hostA: String,
    val hostB: String,
) {
    companion object {
        fun parse(value: String): KubeSpanDetail {
            val parts = value.split('|', limit = 5)
            fun part(i: Int) = parts.getOrElse(i) { "" }
            return KubeSpanDetail(part(0), part(1), part(2).toLongOrNull() ?: 0, part(3), part(4))
        }
    }
}

/** Go only reports a tunnel once its grace is over: every issue alerts at once. */
fun kubespanSeverity(@Suppress("UNUSED_PARAMETER") value: String): String = DATA_CRITICAL

/**
 * The tunnels [health] says are broken, by their stable "idA|idB" key; null when nothing is known
 * (no KubeSpan, or no node read: never an all-clear). On a partial read a tunnel missing from
 * [health] may only be out of sight, so the [known] ones stay until every node answers.
 */
fun kubespanIssuesOf(health: KubeSpanMeshHealth, known: Map<String, String> = emptyMap()): Map<String, String>? = when {
    !health.checked -> null
    health.partial -> (known + health.issues).toSortedMap()
    else -> health.issues.toSortedMap()
}

/** The KubeSpan issues kept by [previous] when it is the same cluster ([context]) and that check read them. */
fun knownKubespanIssues(previous: ClusterSnapshot?, context: String): Map<String, String> =
    previous?.takeIf { it.context == context && it.kubespanWatched && it.kubespanChecked }?.kubespanIssues.orEmpty()
