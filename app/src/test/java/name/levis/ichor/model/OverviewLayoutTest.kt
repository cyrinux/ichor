package name.levis.ichor.model

import name.levis.ichor.model.OverviewCard.ALERTS
import name.levis.ichor.model.OverviewCard.APPS
import name.levis.ichor.model.OverviewCard.ARGO_CD
import name.levis.ichor.model.OverviewCard.DATA_SERVICES
import name.levis.ichor.model.OverviewCard.FLUX
import name.levis.ichor.model.OverviewCard.NODES
import name.levis.ichor.model.OverviewCard.PINNED
import name.levis.ichor.model.OverviewCard.SUMMARY
import name.levis.ichor.model.OverviewCard.TALOS_UPDATE
import name.levis.ichor.model.OverviewCard.TIME_DRIFT
import name.levis.ichor.model.OverviewCard.TOOLS
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class OverviewLayoutTest {

    @Test
    fun defaultShowsEveryCardInOrderButTheKubernetesScreens() {
        val layout = OverviewCard.layout.default
        assertEquals(OverviewCard.entries - TOOLS, layout.visible)
        assertEquals(listOf(TOOLS), layout.hiddenCards)
        assertTrue(layout.isDefault)
    }

    @Test
    fun encodeRoundTrips() {
        val layout = OverviewCard.layout.default.move(7, 0).hide(APPS)
        assertEquals("NODES,PINNED,TALOS_UPDATE,SUMMARY,-APPS,DATA_SERVICES,ARGO_CD,FLUX,TIME_DRIFT,ALERTS,-TOOLS", layout.encode())
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
        assertEquals(listOf(PINNED, TALOS_UPDATE, TIME_DRIFT, NODES, SUMMARY, APPS, DATA_SERVICES, ARGO_CD, FLUX, ALERTS, TOOLS), layout.order)
        assertEquals(setOf(NODES, TOOLS), layout.hidden)
        assertEquals(listOf(PINNED, TALOS_UPDATE, TIME_DRIFT, SUMMARY, APPS, DATA_SERVICES, ARGO_CD, FLUX, ALERTS), layout.visible)
    }

    @Test
    fun aLayoutSavedBeforeTheTalosUpdateCardKeepsItOnTop() {
        // It was pinned above the cards then; other new cards still come last.
        val saved = OverviewCard.layout.parse("NODES,SUMMARY,-APPS,DATA_SERVICES,ARGO_CD,FLUX")
        assertEquals(listOf(PINNED, TALOS_UPDATE, NODES, SUMMARY, DATA_SERVICES, ARGO_CD, FLUX, TIME_DRIFT, ALERTS), saved.visible)
        assertEquals(listOf(PINNED, TALOS_UPDATE), OverviewCard.entries.filter { it.leadsWhenNew })
        // Once saved with it, it stays where it was put.
        val placed = OverviewCard.layout.parse("SUMMARY,-TALOS_UPDATE,NODES")
        assertEquals(listOf(PINNED, SUMMARY, TALOS_UPDATE, NODES), placed.order.take(4))
        assertEquals(setOf(TALOS_UPDATE, TOOLS), placed.hidden)
    }

    @Test
    fun aLayoutSavedBeforeThePinnedCardKeepsItsFormAndGetsItFirst() {
        val saved = OverviewCard.layout.parse("SUMMARY,-APPS,NODES,TALOS_UPDATE")
        assertEquals(listOf(PINNED, SUMMARY, APPS, NODES, TALOS_UPDATE), saved.order.take(5))
        assertEquals(setOf(APPS, TOOLS), saved.hidden)
        // Saved again, the card is one more name of the same form.
        assertEquals("PINNED,SUMMARY,-APPS,NODES,TALOS_UPDATE,", saved.encode().substringBefore("DATA_SERVICES"))
        val moved = OverviewCard.layout.parse("SUMMARY,-PINNED,NODES")
        assertEquals(setOf(PINNED, TOOLS), moved.hidden)
        assertEquals(listOf(TALOS_UPDATE, SUMMARY, PINNED, NODES), moved.order.take(4))
    }

    @Test
    fun moveWorksOnShownCards() {
        val layout = OverviewCard.layout.default.hide(APPS)
        // Shown: PINNED, TALOS_UPDATE, SUMMARY, DATA_SERVICES, ARGO_CD, FLUX, NODES, TIME_DRIFT, ALERTS
        val moved = layout.move(from = 6, to = 3)
        assertEquals(listOf(PINNED, TALOS_UPDATE, SUMMARY, NODES, DATA_SERVICES, ARGO_CD, FLUX, TIME_DRIFT, ALERTS), moved.visible)
        assertEquals(listOf(APPS, TOOLS), moved.hiddenCards)
    }

    @Test
    fun absentCardsAreLeftOutAndKeepTheirPlace() {
        val absent = setOf(DATA_SERVICES, FLUX)
        val layout = OverviewCard.layout.default.hide(ARGO_CD).hide(FLUX)
        assertEquals(listOf(PINNED, TALOS_UPDATE, SUMMARY, APPS, NODES, TIME_DRIFT, ALERTS), layout.visible(absent))
        assertEquals(listOf(ARGO_CD, TOOLS), layout.hiddenCards(absent))
        // APPS (index 3 without DATA_SERVICES) after NODES: DATA_SERVICES stays fifth.
        val moved = layout.move(3, 4, absent)
        assertEquals(listOf(PINNED, TALOS_UPDATE, SUMMARY, NODES, DATA_SERVICES, APPS, TIME_DRIFT, ALERTS), moved.visible)
        assertEquals(setOf(ARGO_CD, FLUX, TOOLS), moved.hidden)
    }

    @Test
    fun moveOutOfRangeIsIgnored() {
        val layout = OverviewCard.layout.default
        assertEquals(layout, layout.move(-1, 2))
        assertEquals(layout, layout.move(0, 10))
        assertEquals(layout, layout.move(2, 2))
    }

    @Test
    fun showPutsTheCardLast() {
        val layout = OverviewCard.layout.default.hide(SUMMARY).hide(NODES)
        val shown = layout.show(SUMMARY)
        assertEquals(listOf(PINNED, TALOS_UPDATE, APPS, DATA_SERVICES, ARGO_CD, FLUX, TIME_DRIFT, ALERTS, SUMMARY), shown.visible)
        assertEquals(listOf(NODES, TOOLS), shown.hiddenCards)
        assertEquals(shown, shown.show(APPS))
    }

    @Test
    fun hideAndShowBackIsNotDefaultUnlessSameOrder() {
        assertFalse(OverviewCard.layout.default.hide(SUMMARY).show(SUMMARY).isDefault)
        assertTrue(OverviewCard.layout.default.hide(ALERTS).show(ALERTS).isDefault)
    }

    @Test
    fun aLayoutSavedBeforeTheKubernetesScreensCardKeepsItHidden() {
        val saved = OverviewCard.layout.parse("SUMMARY,-APPS,NODES")
        assertEquals(setOf(APPS, TOOLS), saved.hidden)
        assertEquals(TOOLS, saved.order.last())
        // Once shown, it is saved without the dash and stays shown.
        val shown = saved.show(TOOLS)
        assertEquals(TOOLS, shown.visible.last())
        assertEquals(shown, OverviewCard.layout.parse(shown.encode()))
    }

    @Test
    fun hiddenByDefaultCards() {
        assertEquals(listOf(TOOLS), OverviewCard.entries.filter { it.hiddenByDefault })
        assertTrue(KubeHomeCard.entries.none { it.hiddenByDefault })
    }

    @Test
    fun detectedCards() {
        assertEquals(setOf(PINNED, DATA_SERVICES, ARGO_CD, FLUX, ALERTS), OverviewCard.entries.filter { it.whenDetected }.toSet())
    }
}
