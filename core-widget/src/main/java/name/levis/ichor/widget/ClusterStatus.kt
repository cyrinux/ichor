package name.levis.ichor.widget

import kotlinx.serialization.Serializable

/** A snapshot older than this is shown dimmed, with its time instead of the status row. */
const val WIDGET_STALE_AFTER_MS = 60 * 60 * 1000L

/** What the widget and the watch show of one background check: counts only, no hostname. */
@Serializable
data class NodeCounts(
    val total: Int,
    val ready: Int,
    val notReady: Int,
    val unreachable: Int,
    /** The etcd alarm list could be read this time (a failed read is never an all-clear). */
    val etcdChecked: Boolean,
    val etcdAlarms: Int,
)

/** One dot + label in the widget's status row. */
sealed interface StatusItem {
    data class NotReady(val count: Int) : StatusItem
    data class Unreachable(val count: Int) : StatusItem
    data object AllReady : StatusItem
    data class Etcd(val alarms: Int) : StatusItem
}

/** How a cluster looks at a glance: the colour of the watch tile and complication. */
enum class Tone { OK, WARN, BAD, UNKNOWN }

/** No check, or one older than [WIDGET_STALE_AFTER_MS]. */
fun isStaleAt(takenAt: Long?, now: Long): Boolean = takenAt == null || now - takenAt > WIDGET_STALE_AFTER_MS

/** Problems first, then etcd (only when it was checked); at most [max] items. */
fun statusItems(c: NodeCounts, max: Int = 2): List<StatusItem> {
    val health = buildList {
        if (c.notReady > 0) add(StatusItem.NotReady(c.notReady))
        if (c.unreachable > 0) add(StatusItem.Unreachable(c.unreachable))
        if (isEmpty() && c.total > 0 && c.ready == c.total) add(StatusItem.AllReady)
    }
    val etcd = if (c.etcdChecked) listOf(StatusItem.Etcd(c.etcdAlarms)) else emptyList()
    return (health + etcd).take(max)
}

/** The colour of [item]'s dot in the widget. */
fun toneOf(item: StatusItem): Tone = when (item) {
    is StatusItem.NotReady -> Tone.WARN
    is StatusItem.Unreachable -> Tone.BAD
    StatusItem.AllReady -> Tone.OK
    is StatusItem.Etcd -> if (item.alarms == 0) Tone.OK else Tone.BAD
}

/** The worst of every status item (not only the two shown); [Tone.UNKNOWN] when stale or empty. */
fun toneOf(c: NodeCounts?, takenAt: Long?, now: Long): Tone {
    if (c == null || isStaleAt(takenAt, now)) return Tone.UNKNOWN
    val tones = statusItems(c, max = Int.MAX_VALUE).map(::toneOf)
    return when {
        Tone.BAD in tones -> Tone.BAD
        Tone.WARN in tones -> Tone.WARN
        Tone.OK in tones -> Tone.OK
        else -> Tone.UNKNOWN
    }
}
