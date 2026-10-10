package name.levis.ichor.widget

import name.levis.ichor.model.ConfigSummary
import name.levis.ichor.monitor.ClusterSnapshot
import name.levis.ichor.monitor.MonitorState
import name.levis.ichor.monitor.clusterFingerprints
import name.levis.ichor.monitor.monitorKeyOf

/** A snapshot older than this is shown dimmed, with its time instead of the status row. */
const val WIDGET_STALE_AFTER_MS = 60 * 60 * 1000L

/** One dot + label in the widget's status row. */
sealed interface StatusItem {
    data class NotReady(val count: Int) : StatusItem
    data class Unreachable(val count: Int) : StatusItem
    data object AllReady : StatusItem
    data class Etcd(val alarms: Int) : StatusItem
}

fun isStale(s: ClusterSnapshot?, now: Long): Boolean = s == null || now - s.takenAt > WIDGET_STALE_AFTER_MS

/**
 * How long until [s] turns stale, or null when it already is: nothing redraws the widget by
 * then unless a refresh is scheduled (the monitor does not run without network).
 */
fun staleInMillis(s: ClusterSnapshot?, now: Long): Long? =
    if (s == null || isStale(s, now)) null else s.takenAt + WIDGET_STALE_AFTER_MS - now + 1

/** Problems first, then etcd (only when it was checked); at most [max] items. */
fun statusItems(s: ClusterSnapshot, max: Int = 2): List<StatusItem> {
    val health = buildList {
        if (s.notReadyCount > 0) add(StatusItem.NotReady(s.notReadyCount))
        if (s.unreachableCount > 0) add(StatusItem.Unreachable(s.unreachableCount))
        if (isEmpty() && s.nodes.isNotEmpty() && s.readyCount == s.nodes.size) add(StatusItem.AllReady)
    }
    val etcd = if (s.etcdChecked) listOf(StatusItem.Etcd(s.etcdAlarms.size)) else emptyList()
    return (health + etcd).take(max)
}

/**
 * Where a widget's snapshot is kept, likeliest first: its [choice] (a context fingerprint), else
 * the cluster on screen ([activeContext] of [summary], or [lastActive] while the config is not
 * loaded); then the other contexts of that cluster, which the monitor may have checked instead.
 */
fun widgetKeys(choice: String?, summary: ConfigSummary?, activeContext: String?, lastActive: String): List<String> {
    val wanted = choice ?: summary?.contexts?.firstOrNull { it.name == activeContext }?.let(::monitorKeyOf) ?: lastActive
    val context = summary?.contexts?.firstOrNull { monitorKeyOf(it) == wanted } ?: return listOf(wanted)
    return listOf(wanted) + (clusterFingerprints(summary, context) - wanted)
}

/**
 * The cluster id a tap on a widget opens (its share link of the cluster screen): that of its
 * [choice]; null for a widget on the cluster on screen, or one not found in [summary] (the app opens as is).
 */
fun widgetClusterId(choice: String?, summary: ConfigSummary?): String? =
    choice?.let { fingerprint -> summary?.contexts?.firstOrNull { monitorKeyOf(it) == fingerprint } }?.clusterId?.takeIf { it.isNotBlank() }

/** The snapshot of the first of [keys] the monitor has one for. */
fun widgetSnapshot(state: MonitorState, keys: List<String>): ClusterSnapshot? = keys.firstNotNullOfOrNull { state.clusters[it] }
