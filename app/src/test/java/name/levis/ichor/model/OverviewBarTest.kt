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
        val bar = OverviewAction.bar.default
        assertEquals(listOf(HEALTH, EVENTS, WORKLOADS), bar.icons)
        assertEquals(listOf(METRICS, KUBESPAN, ETCD, SETTINGS), bar.menu)
        assertTrue(bar.isDefault)
    }

    @Test
    fun movesWithinTheBarAndTheMenu() {
        assertEquals(listOf(EVENTS, HEALTH, WORKLOADS), OverviewAction.bar.default.down(HEALTH).icons)
        assertEquals(listOf(METRICS, ETCD, KUBESPAN, SETTINGS), OverviewAction.bar.default.up(ETCD).menu)
    }

    @Test
    fun movingPastTheLineCrossesIntoTheMenuAndBack() {
        val down = OverviewAction.bar.default.down(WORKLOADS)
        assertEquals(listOf(HEALTH, EVENTS), down.icons)
        assertEquals(listOf(WORKLOADS, METRICS, KUBESPAN, ETCD, SETTINGS), down.menu)
        val up = OverviewAction.bar.default.up(METRICS)
        assertEquals(listOf(HEALTH, EVENTS, WORKLOADS, METRICS), up.icons)
        assertEquals(OverviewAction.bar.default, down.up(WORKLOADS))
    }

    @Test
    fun endsStayPut() {
        val bar = OverviewAction.bar.default
        assertSame(bar, bar.up(HEALTH))
        assertSame(bar, bar.down(SETTINGS))
    }

    @Test
    fun toMenuAndToBarJumpToTheLine() {
        val bar = OverviewAction.bar.default.toMenu(HEALTH)
        assertEquals(listOf(EVENTS, WORKLOADS), bar.icons)
        assertEquals(listOf(HEALTH, METRICS, KUBESPAN, ETCD, SETTINGS), bar.menu)
        val back = OverviewAction.bar.default.toBar(SETTINGS)
        assertEquals(listOf(HEALTH, EVENTS, WORKLOADS, SETTINGS), back.icons)
        assertEquals(listOf(METRICS, KUBESPAN, ETCD), back.menu)
        assertSame(bar, bar.toMenu(METRICS))
        assertSame(back, back.toBar(SETTINGS))
    }

    @Test
    fun encodeRoundTrips() {
        val bar = OverviewAction.bar.default.toBar(ETCD).toMenu(HEALTH)
        assertEquals("EVENTS,WORKLOADS,ETCD|HEALTH,METRICS,KUBESPAN,SETTINGS", bar.encode())
        assertEquals(bar, OverviewAction.bar.parse(bar.encode()))
        val empty = OverviewAction.bar.default.toMenu(HEALTH).toMenu(EVENTS).toMenu(WORKLOADS)
        assertEquals(empty, OverviewAction.bar.parse(empty.encode()))
        assertTrue(empty.icons.isEmpty())
    }

    @Test
    fun parseFallsBackToDefault() {
        assertEquals(OverviewAction.bar.default, OverviewAction.bar.parse(null))
        assertEquals(OverviewAction.bar.default, OverviewAction.bar.parse(""))
        assertEquals(OverviewAction.bar.default, OverviewAction.bar.parse("garbage"))
    }

    @Test
    fun parseSkipsUnknownAndRepeatedAndAppendsMissingToTheMenu() {
        val bar = OverviewAction.bar.parse("SETTINGS,GONE,SETTINGS|ETCD,SETTINGS")
        assertEquals(listOf(SETTINGS), bar.icons)
        assertEquals(listOf(ETCD, HEALTH, EVENTS, WORKLOADS, METRICS, KUBESPAN), bar.menu)
        assertFalse(bar.isDefault)
    }
}
