package name.levis.ichor.model

import name.levis.ichor.model.OverviewCard.ALERTS
import name.levis.ichor.model.OverviewCard.APPS
import name.levis.ichor.model.OverviewCard.ARGO_CD
import name.levis.ichor.model.OverviewCard.DATA_SERVICES
import name.levis.ichor.model.OverviewCard.FLUX
import name.levis.ichor.model.OverviewCard.NODES
import name.levis.ichor.model.OverviewCard.SUMMARY
import name.levis.ichor.model.OverviewCard.TALOS_UPDATE
import name.levis.ichor.model.OverviewCard.TIME_DRIFT
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class OverviewLayoutTest {

    @Test
    fun defaultShowsEveryCardInOrder() {
        val layout = OverviewCard.layout.default
        assertEquals(OverviewCard.entries, layout.visible)
        assertTrue(layout.hiddenCards.isEmpty())
        assertTrue(layout.isDefault)
    }

    @Test
    fun encodeRoundTrips() {
        val layout = OverviewCard.layout.default.move(6, 0).hide(APPS)
        assertEquals("NODES,TALOS_UPDATE,SUMMARY,-APPS,DATA_SERVICES,ARGO_CD,FLUX,TIME_DRIFT,ALERTS", layout.encode())
        assertEquals(layout, OverviewCard.layout.parse(layout.encode()))
    }

    @Test
    fun parseFallsBackToDefault() {
        assertEquals(OverviewCard.layout.default, OverviewCard.layout.parse(null))
        assertEquals(OverviewCard.layout.default, OverviewCard.layout.parse(""))
        assertEquals(OverviewCard.layout.default, OverviewCard.layout.parse("garbage"))
    }

    @Test
    fun parseSkipsUnknownAndRepeatedAndAppendsMissing() {
        val layout = OverviewCard.layout.parse("TIME_DRIFT, -NODES,GONE,TIME_DRIFT,-NODES")
        assertEquals(listOf(TALOS_UPDATE, TIME_DRIFT, NODES, SUMMARY, APPS, DATA_SERVICES, ARGO_CD, FLUX, ALERTS), layout.order)
        assertEquals(setOf(NODES), layout.hidden)
        assertEquals(listOf(TALOS_UPDATE, TIME_DRIFT, SUMMARY, APPS, DATA_SERVICES, ARGO_CD, FLUX, ALERTS), layout.visible)
    }

    @Test
    fun aLayoutSavedBeforeTheTalosUpdateCardKeepsItOnTop() {
        // It was pinned above the cards then; other new cards still come last.
        val saved = OverviewCard.layout.parse("NODES,SUMMARY,-APPS,DATA_SERVICES,ARGO_CD,FLUX")
        assertEquals(listOf(TALOS_UPDATE, NODES, SUMMARY, DATA_SERVICES, ARGO_CD, FLUX, TIME_DRIFT, ALERTS), saved.visible)
        assertEquals(TALOS_UPDATE, OverviewCard.entries.single { it.leadsWhenNew })
        // Once saved with it, it stays where it was put.
        val placed = OverviewCard.layout.parse("SUMMARY,-TALOS_UPDATE,NODES")
        assertEquals(listOf(SUMMARY, TALOS_UPDATE, NODES), placed.order.take(3))
        assertEquals(setOf(TALOS_UPDATE), placed.hidden)
    }

    @Test
    fun moveWorksOnShownCards() {
        val layout = OverviewCard.layout.default.hide(APPS)
        // Shown: TALOS_UPDATE, SUMMARY, DATA_SERVICES, ARGO_CD, FLUX, NODES, TIME_DRIFT, ALERTS
        val moved = layout.move(from = 5, to = 2)
        assertEquals(listOf(TALOS_UPDATE, SUMMARY, NODES, DATA_SERVICES, ARGO_CD, FLUX, TIME_DRIFT, ALERTS), moved.visible)
        assertEquals(listOf(APPS), moved.hiddenCards)
    }

    @Test
    fun absentCardsAreLeftOutAndKeepTheirPlace() {
        val absent = setOf(DATA_SERVICES, FLUX)
        val layout = OverviewCard.layout.default.hide(ARGO_CD).hide(FLUX)
        assertEquals(listOf(TALOS_UPDATE, SUMMARY, APPS, NODES, TIME_DRIFT, ALERTS), layout.visible(absent))
        assertEquals(listOf(ARGO_CD), layout.hiddenCards(absent))
        // APPS (index 2 without DATA_SERVICES) after NODES: DATA_SERVICES stays fourth.
        val moved = layout.move(2, 3, absent)
        assertEquals(listOf(TALOS_UPDATE, SUMMARY, NODES, DATA_SERVICES, APPS, TIME_DRIFT, ALERTS), moved.visible)
        assertEquals(setOf(ARGO_CD, FLUX), moved.hidden)
    }

    @Test
    fun moveOutOfRangeIsIgnored() {
        val layout = OverviewCard.layout.default
        assertEquals(layout, layout.move(-1, 2))
        assertEquals(layout, layout.move(0, 9))
        assertEquals(layout, layout.move(2, 2))
    }

    @Test
    fun showPutsTheCardLast() {
        val layout = OverviewCard.layout.default.hide(SUMMARY).hide(NODES)
        val shown = layout.show(SUMMARY)
        assertEquals(listOf(TALOS_UPDATE, APPS, DATA_SERVICES, ARGO_CD, FLUX, TIME_DRIFT, ALERTS, SUMMARY), shown.visible)
        assertEquals(listOf(NODES), shown.hiddenCards)
        assertEquals(shown, shown.show(APPS))
    }

    @Test
    fun hideAndShowBackIsNotDefaultUnlessSameOrder() {
        assertFalse(OverviewCard.layout.default.hide(SUMMARY).show(SUMMARY).isDefault)
        assertTrue(OverviewCard.layout.default.hide(ALERTS).show(ALERTS).isDefault)
    }

    @Test
    fun detectedCards() {
        assertEquals(setOf(DATA_SERVICES, ARGO_CD, FLUX, ALERTS), OverviewCard.entries.filter { it.whenDetected }.toSet())
    }
}
