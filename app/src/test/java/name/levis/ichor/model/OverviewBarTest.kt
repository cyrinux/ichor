package name.levis.ichor.model

import name.levis.ichor.model.OverviewAction.ALERTS
import name.levis.ichor.model.OverviewAction.ETCD
import name.levis.ichor.model.OverviewAction.EVENTS
import name.levis.ichor.model.OverviewAction.GITOPS
import name.levis.ichor.model.OverviewAction.HEALTH
import name.levis.ichor.model.OverviewAction.KUBESPAN
import name.levis.ichor.model.OverviewAction.METRICS
import name.levis.ichor.model.OverviewAction.REPOSITORY
import name.levis.ichor.model.OverviewAction.SEARCH
import name.levis.ichor.model.OverviewAction.SETTINGS
import name.levis.ichor.model.OverviewAction.WORKLOADS
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test

class OverviewBarTest {

    @Test
    fun defaultKeepsFourIconsWithGitOpsAndEtcd() {
        val bar = OverviewAction.bar.default
        assertEquals(listOf(HEALTH, WORKLOADS, GITOPS, ETCD), bar.icons)
        assertEquals(listOf(EVENTS, METRICS, ALERTS, KUBESPAN, SEARCH, SETTINGS, REPOSITORY), bar.menu)
        assertTrue(bar.isDefault)
    }

    @Test
    fun movesWithinTheBarAndTheMenu() {
        assertEquals(listOf(WORKLOADS, HEALTH, GITOPS, ETCD), OverviewAction.bar.default.down(HEALTH).icons)
        assertEquals(listOf(EVENTS, METRICS, KUBESPAN, ALERTS, SEARCH, SETTINGS, REPOSITORY), OverviewAction.bar.default.up(KUBESPAN).menu)
    }

    @Test
    fun movingPastTheLineCrossesIntoTheMenuAndBack() {
        val down = OverviewAction.bar.default.down(ETCD)
        assertEquals(listOf(HEALTH, WORKLOADS, GITOPS), down.icons)
        assertEquals(listOf(ETCD, EVENTS, METRICS, ALERTS, KUBESPAN, SEARCH, SETTINGS, REPOSITORY), down.menu)
        val up = OverviewAction.bar.default.up(EVENTS)
        assertEquals(listOf(HEALTH, WORKLOADS, GITOPS, ETCD, EVENTS), up.icons)
        assertEquals(OverviewAction.bar.default, down.up(ETCD))
    }

    @Test
    fun endsStayPut() {
        val bar = OverviewAction.bar.default
        assertSame(bar, bar.up(HEALTH))
        assertSame(bar, bar.down(REPOSITORY))
    }

    @Test
    fun toMenuAndToBarJumpToTheLine() {
        val bar = OverviewAction.bar.default.toMenu(HEALTH)
        assertEquals(listOf(WORKLOADS, GITOPS, ETCD), bar.icons)
        assertEquals(listOf(HEALTH, EVENTS, METRICS, ALERTS, KUBESPAN, SEARCH, SETTINGS, REPOSITORY), bar.menu)
        val back = OverviewAction.bar.default.toBar(SETTINGS)
        assertEquals(listOf(HEALTH, WORKLOADS, GITOPS, ETCD, SETTINGS), back.icons)
        assertEquals(listOf(EVENTS, METRICS, ALERTS, KUBESPAN, SEARCH, REPOSITORY), back.menu)
        assertSame(bar, bar.toMenu(METRICS))
        assertSame(back, back.toBar(SETTINGS))
    }

    @Test
    fun encodeRoundTrips() {
        val bar = OverviewAction.bar.default.toBar(KUBESPAN).toMenu(HEALTH)
        assertEquals("WORKLOADS,GITOPS,ETCD,KUBESPAN|HEALTH,EVENTS,METRICS,ALERTS,SEARCH,SETTINGS,REPOSITORY", bar.encode())
        assertEquals(bar, OverviewAction.bar.parse(bar.encode()))
        val empty = OverviewAction.bar.default.toMenu(HEALTH).toMenu(WORKLOADS).toMenu(GITOPS).toMenu(ETCD)
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
        assertEquals(listOf(ETCD, HEALTH, WORKLOADS, GITOPS, EVENTS, METRICS, ALERTS, KUBESPAN, SEARCH, REPOSITORY), bar.menu)
        assertFalse(bar.isDefault)
    }

    @Test
    fun aBarSavedBeforeGitOpsKeepsItsArrangement() {
        val bar = OverviewAction.bar.parse("HEALTH,EVENTS,WORKLOADS|METRICS,KUBESPAN,ETCD,SETTINGS")
        assertEquals(listOf(HEALTH, EVENTS, WORKLOADS), bar.icons)
        assertEquals(listOf(METRICS, KUBESPAN, ETCD, SETTINGS, GITOPS, ALERTS, SEARCH, REPOSITORY), bar.menu)
        assertFalse(bar.isDefault)
    }
}
