package name.levis.talosmobile.widget

import name.levis.talosmobile.monitor.ClusterSnapshot

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
