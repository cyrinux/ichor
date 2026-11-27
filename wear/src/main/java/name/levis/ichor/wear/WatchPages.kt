package name.levis.ichor.wear

import name.levis.ichor.widget.StatusItem
import name.levis.ichor.widget.Tone
import name.levis.ichor.widget.WearCluster
import name.levis.ichor.widget.WearNodeHealth
import name.levis.ichor.widget.isStaleAt
import name.levis.ichor.widget.statusItems
import name.levis.ichor.widget.toneOf

/** The intent extra of [WatchActivity]: the index of the cluster to open in the phone's list. */
const val EXTRA_CLUSTER = "name.levis.ichor.wear.CLUSTER"

/** At most this many issue lines on a cluster's page. */
const val WATCH_MAX_ISSUES = 10

/** What a cluster's row in the list shows: its colour, whether it is stale, and its first problem. */
data class ClusterRow(
    val label: String,
    val tone: Tone,
    val stale: Boolean,
    /** The first problem of the check ("2 down"), or null when all is fine ("OK"). */
    val problem: StatusItem?,
    val takenAt: Long,
)

fun clusterRow(c: WearCluster, now: Long): ClusterRow =
    ClusterRow(c.label, toneOf(c.counts, c.takenAt, now), isStaleAt(c.takenAt, now), clusterIssues(c).firstOrNull(), c.takenAt)

/** The problems of [c]'s last check, the worst first: what is not fine, never "All ready". */
fun clusterIssues(c: WearCluster): List<StatusItem> =
    statusItems(c.counts, max = Int.MAX_VALUE)
        .filter { toneOf(it) != Tone.OK }
        .sortedBy { if (toneOf(it) == Tone.BAD) 0 else 1 }
        .take(WATCH_MAX_ISSUES)

/** A node's dot colour. */
fun nodeTone(health: WearNodeHealth): Tone = when (health) {
    WearNodeHealth.READY -> Tone.OK
    WearNodeHealth.NOT_READY -> Tone.WARN
    WearNodeHealth.UNREACHABLE -> Tone.BAD
}

/** The cluster to open from the tile's [index] extra, or null for the list (no extra, or out of range). */
fun clusterAt(clusters: List<WearCluster>, index: Int): WearCluster? = clusters.getOrNull(index)

/** The newest check among [clusters], or null before any. */
fun lastCheck(clusters: List<WearCluster>): Long? = clusters.map { it.takenAt }.filter { it > 0 }.maxOrNull()
