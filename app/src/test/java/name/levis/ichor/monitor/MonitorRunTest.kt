package name.levis.ichor.monitor

import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.NodeHealth
import name.levis.ichor.model.NodeHealth.READY
import name.levis.ichor.model.NodeHealth.UNREACHABLE
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class MonitorRunTest {

    private val now = 1_800_000_000_000L
    private val lab = ContextSummary(name = "lab", fingerprint = "fl", clusterId = "lab")
    private val prod = ContextSummary(name = "prod", fingerprint = "fp", clusterId = "prod")

    private fun snap(context: ContextSummary, vararg nodes: Pair<String, NodeHealth>) = ClusterSnapshot(
        context = context.name,
        takenAt = now,
        nodes = nodes.associate { (addr, h) -> addr to NodeState("host-$addr", h) },
        fingerprint = context.fingerprint,
    )

    private fun run(before: MonitorState, vararg reads: ClusterRead, enabled: Boolean = true) =
        monitorRun(before, reads.toList(), now, unreachableAlerts = enabled, unreachableRuns = 3, active = "fl")

    @Test
    fun eachClusterIsComparedWithItsOwnSnapshot() {
        // The same node address on both clusters: only prod's went down.
        val before = MonitorState(mapOf("fl" to snap(lab, "10.0.0.2" to READY), "fp" to snap(prod, "10.0.0.2" to READY)))
        val result = run(
            before,
            ClusterRead(lab, snap(lab, "10.0.0.2" to READY)),
            ClusterRead(prod, snap(prod, "10.0.0.2" to NodeHealth.NOT_READY, "10.0.0.3" to READY)),
        )
        assertEquals(listOf("prod"), result.alerts.map { it.context.name })
        assertEquals("node:10.0.0.2", result.alerts.single().alerts.single().key)
        assertEquals(setOf("fl", "fp"), result.state.clusters.keys)
        assertEquals("fl", result.state.active)
    }

    @Test
    fun aNewClusterIsASilentBaseline() {
        val result = run(MonitorState.EMPTY, ClusterRead(prod, snap(prod, "a" to UNREACHABLE)))
        assertTrue(result.alerts.isEmpty())
        assertEquals(snap(prod, "a" to UNREACHABLE).nodes, result.state.clusters.getValue("fp").nodes)
    }

    @Test
    fun aClusterNoLongerReadIsForgotten() {
        val before = MonitorState(mapOf("fl" to snap(lab, "a" to READY), "gone" to snap(prod, "b" to READY)), mapOf("gone" to Reach(2)))
        val result = run(before, ClusterRead(lab, snap(lab, "a" to READY)))
        assertEquals(setOf("fl"), result.state.clusters.keys)
        assertTrue(result.state.reach.isEmpty())
    }

    @Test
    fun anUnreadableClusterKeepsItsSnapshotAndAlertsAtTheThirdRun() {
        var state = MonitorState(mapOf("fp" to snap(prod, "a" to READY)))
        val alerts = (1..4).map {
            val result = run(state, ClusterRead(prod, snapshot = null))
            state = result.state
            result.alerts.flatMap { it.alerts }
        }
        assertEquals(listOf(0, 0, 1, 0), alerts.map { it.size })
        assertEquals(AlertKind.CLUSTER_UNREACHABLE, alerts[2].single().kind)
        assertEquals(snap(prod, "a" to READY), state.clusters["fp"])
    }

    @Test
    fun noNodeAnsweringCountsAsUnreachableAndNodeAlertsStaySilent() {
        var state = MonitorState(mapOf("fp" to snap(prod, "a" to READY, "b" to READY)), mapOf("fp" to Reach(2)))
        val down = run(state, ClusterRead(prod, snap(prod, "a" to UNREACHABLE, "b" to UNREACHABLE)))
        assertEquals(listOf(AlertKind.CLUSTER_UNREACHABLE), down.alerts.flatMap { it.alerts }.map { it.kind })
        state = down.state
        val back = run(state, ClusterRead(prod, snap(prod, "a" to READY, "b" to READY)))
        assertEquals(listOf(AlertKind.CLUSTER_REACHABLE), back.alerts.flatMap { it.alerts }.map { it.kind })
        assertNull(back.state.reach["fp"])
    }

    @Test
    fun aSkippedClusterKeepsEverything() {
        val before = MonitorState(mapOf("fp" to snap(prod, "a" to READY)), mapOf("fp" to Reach(2)))
        val result = run(before, ClusterRead(prod, snapshot = null, skipped = true))
        assertEquals(before.clusters, result.state.clusters)
        assertEquals(before.reach, result.state.reach)
        assertTrue(result.alerts.isEmpty())
    }

    @Test
    fun withTheAlertOffNothingIsPosted() {
        val before = MonitorState(emptyMap(), mapOf("fp" to Reach(5)))
        assertTrue(run(before, ClusterRead(prod, snapshot = null), enabled = false).alerts.isEmpty())
    }
}
