package name.levis.ichor.widget

import name.levis.ichor.model.ConfigSummary
import name.levis.ichor.monitor.ClusterSnapshot
import name.levis.ichor.monitor.MonitorState
import name.levis.ichor.monitor.clusterFingerprints
import name.levis.ichor.monitor.monitorKeyOf

// WIDGET_STALE_AFTER_MS, StatusItem and the counts-based rules live in :core-widget, shared
// with the Wear OS tile.

/** [s]'s node and etcd counts, all the widget and the watch show of it. */
fun nodeCounts(s: ClusterSnapshot): NodeCounts =
    NodeCounts(s.nodes.size, s.readyCount, s.notReadyCount, s.unreachableCount, s.etcdChecked, s.etcdAlarms.size)

fun isStale(s: ClusterSnapshot?, now: Long): Boolean = isStaleAt(s?.takenAt, now)

/**
 * How long until [s] turns stale, or null when it already is: nothing redraws the widget by
 * then unless a refresh is scheduled (the monitor does not run without network).
 */
fun staleInMillis(s: ClusterSnapshot?, now: Long): Long? =
    if (s == null || isStale(s, now)) null else s.takenAt + WIDGET_STALE_AFTER_MS - now + 1

/** Problems first, then etcd (only when it was checked); at most [max] items. */
fun statusItems(s: ClusterSnapshot, max: Int = 2): List<StatusItem> = statusItems(nodeCounts(s), max)

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

/** The name the user gave [s]'s cluster, as in the app, except in screenshot mode ([masked]); else its context. */
fun widgetLabel(s: ClusterSnapshot, names: Map<String, String>, masked: Boolean): String =
    names[s.fingerprint].takeIf { !masked && s.fingerprint.isNotBlank() } ?: s.context

/** The snapshot of the first of [keys] the monitor has one for. */
fun widgetSnapshot(state: MonitorState, keys: List<String>): ClusterSnapshot? = keys.firstNotNullOfOrNull { state.clusters[it] }
