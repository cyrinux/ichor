package name.levis.ichor.widget

import name.levis.ichor.model.FleetCounts
import name.levis.ichor.model.FleetFinding
import name.levis.ichor.model.FleetRow
import name.levis.ichor.model.FleetSummary
import name.levis.ichor.model.FleetTone
import name.levis.ichor.monitor.FleetGlance
import name.levis.ichor.monitor.FleetGlanceRow
import name.levis.ichor.monitor.decodeMonitorState
import name.levis.ichor.monitor.fleetGlanceOf
import name.levis.ichor.monitor.tallyAt
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class FleetWidgetTest {

    private val now = 1_800_000_000_000L
    private val minute = 60_000L

    private fun row(key: String, tone: FleetTone, age: Long = 5 * minute, finding: FleetFinding? = null) =
        FleetGlanceRow(key = key, name = key, tone = tone, checkedAt = if (age < 0) 0 else now - age, finding = finding)

    private val down = FleetFinding("nodes", "critical", "node-b unreachable")
    private val glance = FleetGlance(
        listOf(
            row("prod-eu", FleetTone.BAD, finding = down),
            row("staging", FleetTone.WARN, finding = FleetFinding("gitops", "warning", "shop out of sync", "argocd")),
            row("edge", FleetTone.UNKNOWN, age = -1),
            row("lab", FleetTone.OK),
            row("dev", FleetTone.OK),
            row("ci", FleetTone.OK),
        ),
    )

    @Test
    fun countsTheClustersNeedingAttentionWorstFirst() {
        val view = fleetWidgetView(glance, now)!!
        assertEquals(2, view.attention)
        assertEquals(6, view.total)
        assertEquals(FleetTone.BAD, view.tone)
        assertEquals(listOf("prod-eu", "staging"), view.rows.map { it.key })
        assertEquals(1, view.unchecked)
        assertFalse(view.allStale)
    }

    @Test
    fun listsAtMostTheRowsThatFit() {
        val many = FleetGlance(List(5) { row("bad-$it", FleetTone.BAD) })
        assertEquals(3, fleetWidgetView(many, now)!!.rows.size)
        assertEquals(1, fleetWidgetView(many, now, maxRows = 1)!!.rows.size)
        assertEquals(0, fleetWidgetRowsFor(90f))
        assertEquals(1, fleetWidgetRowsFor(110f))
        assertEquals(3, fleetWidgetRowsFor(400f))
    }

    @Test
    fun aFineFleetHasNoRows() {
        val fine = FleetGlance(listOf(row("lab", FleetTone.OK), row("dev", FleetTone.OK)))
        val view = fleetWidgetView(fine, now)!!
        assertEquals(0, view.attention)
        assertEquals(FleetTone.OK, view.tone)
        assertTrue(view.rows.isEmpty())
        assertEquals(0, view.unchecked)
    }

    @Test
    fun aCheckTurnedStaleSinceTheRankingReadsAsUnknown() {
        val later = now + WIDGET_STALE_AFTER_MS
        val view = fleetWidgetView(glance, later)!!
        assertEquals(0, view.attention)
        assertEquals(FleetTone.UNKNOWN, view.tone)
        assertTrue(view.allStale)
        // The oldest check: the never-checked one does not count.
        assertEquals(now - 5 * minute, view.oldestCheck)
    }

    @Test
    fun noRankedCheckYetShowsNothing() {
        assertNull(fleetWidgetView(FleetGlance(), now))
    }

    @Test
    fun theGlanceKeepsTheRankingAndHidesMaskedSubjects() {
        val summary = FleetSummary(
            clusters = listOf(
                FleetRow(key = "a", name = "cluster-1", context = "c1", tone = FleetTone.BAD, checkedAt = now, topFinding = down),
                FleetRow(
                    key = "b",
                    name = "cluster-2",
                    context = "c2",
                    tone = FleetTone.WARN,
                    checkedAt = now,
                    topFinding = FleetFinding("alertmanager", "warning", "KubePodCrashLooping · shop/checkout"),
                ),
            ),
            counts = FleetCounts(bad = 1, warn = 1),
            worst = FleetTone.BAD,
        )
        val shown = fleetGlanceOf(summary, masked = false)
        assertEquals(listOf("a", "b"), shown.rows.map { it.key })
        assertEquals("KubePodCrashLooping · shop/checkout", shown.rows[1].finding?.subject)
        val masked = fleetGlanceOf(summary, masked = true)
        // Screenshot mode: a generic subject stays, one naming the user's objects goes.
        assertEquals("node-b unreachable", masked.rows[0].finding?.subject)
        assertEquals("", masked.rows[1].finding?.subject)
        assertEquals("alertmanager", masked.rows[1].finding?.track)
    }

    @Test
    fun theTallyCountsEachToneAtTheTime() {
        assertEquals(FleetTally(bad = 1, warn = 1, unknown = 1, ok = 3, total = 6, takenAt = now - 5 * minute), glance.tallyAt(now))
        val later = glance.tallyAt(now + WIDGET_STALE_AFTER_MS)
        assertEquals(6, later.unknown)
        assertEquals(0, later.needAttention)
    }

    @Test
    fun anOlderStoredStateLoadsWithAnEmptyFleet() {
        val state = decodeMonitorState("""{"clusters":{},"active":"fl"}""")!!
        assertEquals(FleetGlance(), state.fleet)
        assertEquals("fl", state.active)
    }
}
