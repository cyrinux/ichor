package name.levis.ichor.quicksettings

import name.levis.ichor.model.ConfigSummary
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.NodeHealth
import name.levis.ichor.model.NodeHealth.NOT_READY
import name.levis.ichor.model.NodeHealth.READY
import name.levis.ichor.model.NodeHealth.UNREACHABLE
import name.levis.ichor.monitor.ClusterSnapshot
import name.levis.ichor.monitor.MonitorState
import name.levis.ichor.monitor.NodeState
import org.junit.Assert.assertEquals
import org.junit.Test

class TileStateTest {
    private val lab = ContextSummary(name = "lab-admin", fingerprint = "fa", clusterId = "lab")
    private val summary = ConfigSummary(current = "lab-admin", contexts = listOf(lab))

    private fun snap(vararg health: NodeHealth) = ClusterSnapshot(
        context = "lab-admin",
        takenAt = 1_800_000_000_000L,
        nodes = health.withIndex().associate { (i, h) -> "192.0.2.$i" to NodeState("n$i", h) },
        fingerprint = "fa",
    )

    @Test
    fun everyNodeReadyIsReady() {
        val view = tileView(MonitorState(mapOf("fa" to snap(READY, READY))), summary, "lab-admin", emptyMap(), masked = false)
        assertEquals(TileView(TileHealth.READY, "lab-admin", ready = 2, total = 2), view)
    }

    @Test
    fun aNodeDownIsDegradedWithTheCounts() {
        val view = tileView(MonitorState(mapOf("fa" to snap(READY, NOT_READY, UNREACHABLE))), summary, "lab-admin", emptyMap(), masked = false)
        assertEquals(TileView(TileHealth.DEGRADED, "lab-admin", ready = 1, total = 3), view)
    }

    @Test
    fun theNameTheUserGaveShowsExceptInScreenshotMode() {
        val state = MonitorState(mapOf("fa" to snap(READY)))
        assertEquals("Home lab", tileView(state, summary, "lab-admin", mapOf("fa" to "Home lab"), masked = false).label)
        assertEquals("lab-admin", tileView(state, summary, "lab-admin", mapOf("fa" to "Home lab"), masked = true).label)
    }

    @Test
    fun aClusterNotCheckedYetSaysSo() {
        assertEquals(TileView(TileHealth.NO_CHECK, null, 0, 0), tileView(MonitorState.EMPTY, summary, "lab-admin", emptyMap(), masked = false))
    }

    @Test
    fun noClusterAtAllIsUnavailable() {
        assertEquals(TileHealth.NO_CLUSTER, tileView(MonitorState.EMPTY, ConfigSummary(current = "", contexts = emptyList()), null, emptyMap(), masked = false).health)
        assertEquals(TileHealth.NO_CLUSTER, tileView(MonitorState.EMPTY, null, null, emptyMap(), masked = false).health)
    }

    @Test
    fun theConfigLockedStillShowsTheLastClusterOnScreen() {
        // The app lock keeps the config sealed: the monitor's last active cluster stands in for it.
        val state = MonitorState(mapOf("fa" to snap(READY)), active = "fa")
        assertEquals(TileHealth.READY, tileView(state, null, null, emptyMap(), masked = false).health)
    }
}
