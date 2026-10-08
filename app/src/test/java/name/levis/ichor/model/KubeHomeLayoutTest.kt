package name.levis.ichor.model

import name.levis.ichor.model.KubeHomeCard.APPS
import name.levis.ichor.model.KubeHomeCard.ARGO_CD
import name.levis.ichor.model.KubeHomeCard.DATA_SERVICES
import name.levis.ichor.model.KubeHomeCard.FLUX
import name.levis.ichor.model.KubeHomeCard.NODES
import name.levis.ichor.model.KubeHomeCard.SUMMARY
import name.levis.ichor.model.KubeHomeCard.TOOLS
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class KubeHomeLayoutTest {

    @Test
    fun defaultShowsEveryCardInOrder() {
        val layout = KubeHomeCard.layout.default
        assertEquals(listOf(SUMMARY, APPS, NODES, TOOLS, DATA_SERVICES, ARGO_CD, FLUX), layout.visible)
        assertTrue(layout.hiddenCards.isEmpty())
        assertTrue(layout.isDefault)
    }

    @Test
    fun encodeRoundTrips() {
        val layout = KubeHomeCard.layout.default.move(3, 0).hide(NODES)
        assertEquals("TOOLS,SUMMARY,APPS,-NODES,DATA_SERVICES,ARGO_CD,FLUX", layout.encode())
        assertEquals(layout, KubeHomeCard.layout.parse(layout.encode()))
        assertFalse(layout.isDefault)
    }

    @Test
    fun parseFallsBackToDefaultAndSkipsTheOverviewsCards() {
        assertEquals(KubeHomeCard.layout.default, KubeHomeCard.layout.parse(null))
        assertEquals(KubeHomeCard.layout.default, KubeHomeCard.layout.parse(""))
        // A layout saved by the Talos overview: its own cards are unknown here, the shared names kept.
        val layout = KubeHomeCard.layout.parse("TALOS_UPDATE,-APPS,NODES,-DATA_SERVICES,TIME_DRIFT")
        assertEquals(listOf(APPS, NODES, DATA_SERVICES, SUMMARY, TOOLS, ARGO_CD, FLUX), layout.order)
        assertEquals(setOf(APPS, DATA_SERVICES), layout.hidden)
    }

    @Test
    fun absentCardsAreLeftOutAndKeepTheirPlace() {
        val absent = setOf(ARGO_CD, FLUX)
        val layout = KubeHomeCard.layout.default.hide(DATA_SERVICES)
        assertEquals(listOf(SUMMARY, APPS, NODES, TOOLS), layout.visible(absent))
        assertEquals(listOf(DATA_SERVICES), layout.hiddenCards(absent))
        assertEquals(listOf(NODES, SUMMARY, APPS, TOOLS, ARGO_CD, FLUX), layout.move(2, 0, absent).visible)
    }

    @Test
    fun showPutsTheCardLast() {
        val shown = KubeHomeCard.layout.default.hide(SUMMARY).hide(TOOLS).show(SUMMARY)
        assertEquals(listOf(APPS, NODES, DATA_SERVICES, ARGO_CD, FLUX, SUMMARY), shown.visible)
        assertEquals(listOf(TOOLS), shown.hiddenCards)
    }

    @Test
    fun detectedCardsAndNoLeadingOne() {
        assertEquals(setOf(DATA_SERVICES, ARGO_CD, FLUX), KubeHomeCard.entries.filter { it.whenDetected }.toSet())
        assertTrue(KubeHomeCard.entries.none { it.leadsWhenNew })
    }
}
