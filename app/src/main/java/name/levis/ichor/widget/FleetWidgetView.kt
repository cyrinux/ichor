package name.levis.ichor.widget

import name.levis.ichor.model.FleetTone
import name.levis.ichor.monitor.FleetGlance
import name.levis.ichor.monitor.FleetGlanceRow
import name.levis.ichor.monitor.toneAt

/** The most "name · finding" rows the fleet widget lists. */
const val FLEET_WIDGET_MAX_ROWS = 3

/**
 * What the fleet widget draws at a time: [attention] of [total] clusters need a look, [tone] the
 * worst one's (the count's colour), [rows] the first of those, worst first; [unchecked] the
 * clusters not checked lately. [allStale] when no cluster was checked within the hour: the
 * widget dims and says how old the oldest check is ([oldestCheck], unix ms, 0 for none).
 */
data class FleetWidgetView(
    val attention: Int,
    val total: Int,
    val tone: FleetTone,
    val rows: List<FleetGlanceRow>,
    val unchecked: Int,
    val allStale: Boolean,
    val oldestCheck: Long,
)

/** The widget's content for [glance] at [now], with at most [maxRows] rows; null before the first ranked check. */
fun fleetWidgetView(glance: FleetGlance, now: Long, maxRows: Int = FLEET_WIDGET_MAX_ROWS): FleetWidgetView? {
    if (glance.rows.isEmpty()) return null
    val toned = glance.rows.map { it to it.toneAt(now) }
    // FleetTone is declared worst first; a stable sort keeps the core's order within a tone.
    val needing = toned.filter { (_, tone) -> tone == FleetTone.BAD || tone == FleetTone.WARN }.sortedBy { (_, tone) -> tone.ordinal }
    return FleetWidgetView(
        attention = needing.size,
        total = glance.rows.size,
        tone = toned.minOf { (_, tone) -> tone.ordinal }.let { FleetTone.entries[it] },
        rows = needing.take(maxRows.coerceAtLeast(0)).map { (row, _) -> row },
        unchecked = toned.count { (_, tone) -> tone == FleetTone.UNKNOWN },
        allStale = glance.rows.all { isStaleAt(it.checkedAt.takeIf { at -> at > 0 }, now) },
        oldestCheck = glance.rows.mapNotNull { row -> row.checkedAt.takeIf { it > 0 } }.minOrNull() ?: 0,
    )
}

/** How many "name · finding" rows fit under the count in a widget [heightDp] tall. */
fun fleetWidgetRowsFor(heightDp: Float): Int = ((heightDp - 76f) / 18f).toInt().coerceIn(0, FLEET_WIDGET_MAX_ROWS)
