package name.levis.ichor.monitor

import name.levis.ichor.model.TalosContainersHealth

/** The container a ContainerConfig declares has failed: Talos does not start it again. */
const val TALOS_CONTAINER_FAILED = "failed"

/**
 * A Talos container issue's stored value as Go writes it, split:
 * "severity|state|restartCount|exitCode|hostname|name". Never its error text nor its last log:
 * they are workload output, not for a notification.
 */
data class TalosContainerDetail(
    val severity: String,
    val state: String,
    val restartCount: Long,
    val exitCode: Int,
    val hostname: String,
    val name: String,
) {
    val failed: Boolean get() = state == TALOS_CONTAINER_FAILED

    companion object {
        fun parse(value: String): TalosContainerDetail {
            val parts = value.split('|', limit = 6)
            fun part(i: Int) = parts.getOrElse(i) { "" }
            return TalosContainerDetail(
                severity = part(0),
                state = part(1),
                restartCount = part(2).toLongOrNull() ?: 0,
                exitCode = part(3).toIntOrNull() ?: 0,
                hostname = part(4),
                name = part(5),
            )
        }
    }
}

/** A failed container alerts at once; one that keeps restarting on its second check in a row. */
fun talosContainerSeverity(value: String): String =
    if (TalosContainerDetail.parse(value).severity == DATA_CRITICAL) DATA_CRITICAL else DATA_WARNING

/**
 * The Talos containers [health] says are in trouble, by their "node|name" key; null when no
 * node answered (never an all-clear). A node that did not answer keeps its [known] issues.
 */
fun talosContainerIssuesOf(health: TalosContainersHealth, known: Map<String, String> = emptyMap()): Map<String, String>? {
    if (!health.checked) return null
    val unread = health.unread.toSet()
    return (known.filterKeys { it.substringBefore('|') in unread } + health.issues).toSortedMap()
}

/** The Talos container issues kept by [previous] when it is the same cluster ([context]) and that check read them. */
fun knownTalosContainerIssues(previous: ClusterSnapshot?, context: String): Map<String, String> =
    previous?.takeIf { it.context == context && it.talosContainerWatched && it.talosContainerChecked }?.talosContainerIssues.orEmpty()
