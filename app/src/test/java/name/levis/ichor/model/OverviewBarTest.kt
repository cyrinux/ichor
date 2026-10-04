package name.levis.ichor.model

import name.levis.ichor.model.OverviewAction.ETCD
import name.levis.ichor.model.OverviewAction.EVENTS
import name.levis.ichor.model.OverviewAction.HEALTH
import name.levis.ichor.model.OverviewAction.KUBESPAN
import name.levis.ichor.model.OverviewAction.METRICS
import name.levis.ichor.model.OverviewAction.SETTINGS
import name.levis.ichor.model.OverviewAction.WORKLOADS
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test

class OverviewBarTest {

    @Test
    fun defaultKeepsThreeIconsAndTheRestInTheMenu() {
        val bar = OverviewBar()
        assertEquals(listOf(HEALTH, EVENTS, WORKLOADS), bar.icons)
        assertEquals(listOf(METRICS, KUBESPAN, ETCD, SETTINGS), bar.menu)
        assertTrue(bar.isDefault)
    }

    @Test
    fun movesWithinTheBarAndTheMenu() {
        assertEquals(listOf(EVENTS, HEALTH, WORKLOADS), OverviewBar().down(HEALTH).icons)
        assertEquals(listOf(METRICS, ETCD, KUBESPAN, SETTINGS), OverviewBar().up(ETCD).menu)
    }

    @Test
    fun movingPastTheLineCrossesIntoTheMenuAndBack() {
        val down = OverviewBar().down(WORKLOADS)
        assertEquals(listOf(HEALTH, EVENTS), down.icons)
        assertEquals(listOf(WORKLOADS, METRICS, KUBESPAN, ETCD, SETTINGS), down.menu)
        val up = OverviewBar().up(METRICS)
        assertEquals(listOf(HEALTH, EVENTS, WORKLOADS, METRICS), up.icons)
        assertEquals(OverviewBar(), down.up(WORKLOADS))
    }

    @Test
    fun endsStayPut() {
        val bar = OverviewBar()
        assertSame(bar, bar.up(HEALTH))
        assertSame(bar, bar.down(SETTINGS))
    }

    @Test
    fun toMenuAndToBarJumpToTheLine() {
        val bar = OverviewBar().toMenu(HEALTH)
        assertEquals(listOf(EVENTS, WORKLOADS), bar.icons)
        assertEquals(listOf(HEALTH, METRICS, KUBESPAN, ETCD, SETTINGS), bar.menu)
        val back = OverviewBar().toBar(SETTINGS)
        assertEquals(listOf(HEALTH, EVENTS, WORKLOADS, SETTINGS), back.icons)
        assertEquals(listOf(METRICS, KUBESPAN, ETCD), back.menu)
        assertSame(bar, bar.toMenu(METRICS))
        assertSame(back, back.toBar(SETTINGS))
    }

    @Test
    fun encodeRoundTrips() {
        val bar = OverviewBar().toBar(ETCD).toMenu(HEALTH)
        assertEquals("EVENTS,WORKLOADS,ETCD|HEALTH,METRICS,KUBESPAN,SETTINGS", bar.encode())
        assertEquals(bar, OverviewBar.parse(bar.encode()))
        val empty = OverviewBar().toMenu(HEALTH).toMenu(EVENTS).toMenu(WORKLOADS)
        assertEquals(empty, OverviewBar.parse(empty.encode()))
        assertTrue(empty.icons.isEmpty())
    }

    @Test
    fun parseFallsBackToDefault() {
        assertEquals(OverviewBar(), OverviewBar.parse(null))
        assertEquals(OverviewBar(), OverviewBar.parse(""))
        assertEquals(OverviewBar(), OverviewBar.parse("garbage"))
    }

    @Test
    fun parseSkipsUnknownAndRepeatedAndAppendsMissingToTheMenu() {
        val bar = OverviewBar.parse("SETTINGS,GONE,SETTINGS|ETCD,SETTINGS")
        assertEquals(listOf(SETTINGS), bar.icons)
        assertEquals(listOf(ETCD, HEALTH, EVENTS, WORKLOADS, METRICS, KUBESPAN), bar.menu)
        assertFalse(bar.isDefault)
    }
}
