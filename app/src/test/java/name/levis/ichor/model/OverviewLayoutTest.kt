package name.levis.ichor.model

import name.levis.ichor.model.OverviewCard.APPS
import name.levis.ichor.model.OverviewCard.ARGO_CD
import name.levis.ichor.model.OverviewCard.DATA_SERVICES
import name.levis.ichor.model.OverviewCard.NODES
import name.levis.ichor.model.OverviewCard.SUMMARY
import name.levis.ichor.model.OverviewCard.TIME_DRIFT
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class OverviewLayoutTest {

    @Test
    fun defaultShowsEveryCardInOrder() {
        val layout = OverviewLayout()
        assertEquals(OverviewCard.entries, layout.visible)
        assertTrue(layout.hiddenCards.isEmpty())
        assertTrue(layout.isDefault)
    }

    @Test
    fun encodeRoundTrips() {
        val layout = OverviewLayout().move(4, 0).hide(APPS)
        assertEquals("NODES,SUMMARY,-APPS,DATA_SERVICES,ARGO_CD,TIME_DRIFT", layout.encode())
        assertEquals(layout, OverviewLayout.parse(layout.encode()))
    }

    @Test
    fun parseFallsBackToDefault() {
        assertEquals(OverviewLayout(), OverviewLayout.parse(null))
        assertEquals(OverviewLayout(), OverviewLayout.parse(""))
        assertEquals(OverviewLayout(), OverviewLayout.parse("garbage"))
    }

    @Test
    fun parseSkipsUnknownAndRepeatedAndAppendsMissing() {
        val layout = OverviewLayout.parse("TIME_DRIFT, -NODES,GONE,TIME_DRIFT,-NODES")
        assertEquals(listOf(TIME_DRIFT, NODES, SUMMARY, APPS, DATA_SERVICES, ARGO_CD), layout.order)
        assertEquals(setOf(NODES), layout.hidden)
        assertEquals(listOf(TIME_DRIFT, SUMMARY, APPS, DATA_SERVICES, ARGO_CD), layout.visible)
    }

    @Test
    fun moveWorksOnShownCards() {
        val layout = OverviewLayout().hide(APPS)
        // Shown: SUMMARY, DATA_SERVICES, ARGO_CD, NODES, TIME_DRIFT
        val moved = layout.move(from = 3, to = 1)
        assertEquals(listOf(SUMMARY, NODES, DATA_SERVICES, ARGO_CD, TIME_DRIFT), moved.visible)
        assertEquals(listOf(APPS), moved.hiddenCards)
    }

    @Test
    fun moveOutOfRangeIsIgnored() {
        val layout = OverviewLayout()
        assertEquals(layout, layout.move(-1, 2))
        assertEquals(layout, layout.move(0, 6))
        assertEquals(layout, layout.move(2, 2))
    }

    @Test
    fun showPutsTheCardLast() {
        val layout = OverviewLayout().hide(SUMMARY).hide(NODES)
        val shown = layout.show(SUMMARY)
        assertEquals(listOf(APPS, DATA_SERVICES, ARGO_CD, TIME_DRIFT, SUMMARY), shown.visible)
        assertEquals(listOf(NODES), shown.hiddenCards)
        assertEquals(shown, shown.show(APPS))
    }

    @Test
    fun hideAndShowBackIsNotDefaultUnlessSameOrder() {
        assertFalse(OverviewLayout().hide(SUMMARY).show(SUMMARY).isDefault)
        assertTrue(OverviewLayout().hide(TIME_DRIFT).show(TIME_DRIFT).isDefault)
    }

    @Test
    fun detectedCards() {
        assertEquals(setOf(DATA_SERVICES, ARGO_CD), OverviewCard.entries.filter { it.whenDetected }.toSet())
    }
}
