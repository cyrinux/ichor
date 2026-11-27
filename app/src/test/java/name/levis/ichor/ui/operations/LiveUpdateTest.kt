package name.levis.ichor.ui.operations

import name.levis.ichor.data.ClusterUpgradeRunState
import name.levis.ichor.model.ClusterUpgradeProgress
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class LiveUpdateTest {
    private val text = raw("step")

    @Test
    fun aClusterUpgradeHasASegmentPerNodeItsCountAndPauseThenResume() {
        val p = ClusterUpgradeProgress(phase = ClusterUpgradeProgress.NODE, index = 2, total = 7, hostname = "w-2", nodePhase = "installing")
        val run = ClusterUpgradeRunState("v1.11.0", "lab", p)
        val live = liveUpdateOf(ClusterUpgradePresenter.progress(run))
        assertEquals(List(7) { 1 }, live.segments)
        assertEquals(2, live.done)
        assertFalse(live.indeterminate)
        assertEquals("2/7", live.chip)
        assertEquals(listOf(OperationAction.PAUSE, OperationAction.ABORT), live.actions)

        val paused = run.copy(progress = p.copy(phase = ClusterUpgradeProgress.PAUSED))
        assertEquals(listOf(OperationAction.RESUME, OperationAction.ABORT), liveUpdateOf(ClusterUpgradePresenter.progress(paused)).actions)
    }

    @Test
    fun aRunThatDoesNotCountSpinsWithoutAChip() {
        val live = liveUpdateOf(OperationNotification(text, text, actions = listOf(OperationAction.STOP)))
        assertTrue(live.indeterminate)
        assertTrue(live.hasBar)
        assertEquals(emptyList<Int>(), live.segments)
        assertNull(live.chip)
        assertEquals(listOf(OperationAction.STOP), live.actions)
    }

    @Test
    fun aRunWithoutABarHasNone() {
        val live = liveUpdateOf(OperationNotification(text, text, progress = null))
        assertFalse(live.hasBar)
        assertNull(live.chip)
    }

    @Test
    fun aLongRunIsOneSegmentAndTheCountStaysInRange() {
        val long = liveUpdateOf(OperationNotification(text, text, progress = OperationProgress(40, 30)))
        assertEquals(listOf(30), long.segments)
        assertEquals(30, long.done)
        assertEquals("30/30", long.chip)

        val atLimit = liveUpdateOf(OperationNotification(text, text, progress = OperationProgress(0, LIVE_UPDATE_MAX_SEGMENTS)))
        assertEquals(LIVE_UPDATE_MAX_SEGMENTS, atLimit.segments.size)
        assertEquals("0/$LIVE_UPDATE_MAX_SEGMENTS", atLimit.chip)
    }

    @Test
    fun aCountdownLeavesTheChipToTheTimer() {
        val live = liveUpdateOf(OperationNotification(text, text, progress = OperationProgress(1, 3), countdownTo = 1_000L))
        assertNull(live.chip)
        assertEquals(List(3) { 1 }, live.segments)
    }
}
