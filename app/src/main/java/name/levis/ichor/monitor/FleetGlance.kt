package name.levis.ichor.monitor

import kotlinx.serialization.Serializable
import name.levis.ichor.model.FleetFinding
import name.levis.ichor.model.FleetSummary
import name.levis.ichor.model.FleetTone
import name.levis.ichor.model.subjectShown
import name.levis.ichor.widget.FleetTally
import name.levis.ichor.widget.isStaleAt

/**
 * One cluster of the fleet as the monitor's last run ranked it: [name] as shown (masked in
 * screenshot mode), [tone] when it was ranked, [checkedAt] its check's time (unix ms, 0:
 * never), [finding] the top finding, its subject left out when screenshot mode hides it.
 */
@Serializable
data class FleetGlanceRow(
    val key: String,
    val name: String,
    val tone: FleetTone = FleetTone.UNKNOWN,
    val checkedAt: Long = 0,
    val finding: FleetFinding? = null,
)

/**
 * The fleet screen's ranking (worst first) kept after each background check, so the fleet
 * widget and the watch never call the Go core: no node, issue map or endpoint, only what they
 * draw. Empty before the first check (and in a file of an older version).
 */
@Serializable
data class FleetGlance(val rows: List<FleetGlanceRow> = emptyList())

/** The compact [FleetGlance] of [summary]; a finding's subject only when [masked] lets it show. */
fun fleetGlanceOf(summary: FleetSummary, masked: Boolean): FleetGlance = FleetGlance(
    summary.clusters.map { row ->
        val finding = row.topFinding?.let { if (it.subjectShown(masked)) it else it.copy(subject = "") }
        FleetGlanceRow(row.key, row.name, row.tone, row.checkedAt, finding)
    },
)

/** The row's tone at [now]: a check that turned stale since it was ranked reads as unknown, as the Go core ranks it. */
fun FleetGlanceRow.toneAt(now: Long): FleetTone = if (isStaleAt(checkedAt.takeIf { it > 0 }, now)) FleetTone.UNKNOWN else tone

/** The count per tone at [now], with the newest check's time (what the watch gets). */
fun FleetGlance.tallyAt(now: Long): FleetTally {
    val tones = rows.groupingBy { it.toneAt(now) }.eachCount()
    return FleetTally(
        bad = tones[FleetTone.BAD] ?: 0,
        warn = tones[FleetTone.WARN] ?: 0,
        unknown = tones[FleetTone.UNKNOWN] ?: 0,
        ok = tones[FleetTone.OK] ?: 0,
        total = rows.size,
        takenAt = rows.maxOfOrNull { it.checkedAt } ?: 0,
    )
}
