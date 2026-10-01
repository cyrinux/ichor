package name.levis.talosmobile.widget

import name.levis.talosmobile.model.NodeHealth
import name.levis.talosmobile.model.NodeHealth.NOT_READY
import name.levis.talosmobile.model.NodeHealth.READY
import name.levis.talosmobile.model.NodeHealth.UNREACHABLE
import name.levis.talosmobile.monitor.ClusterSnapshot
import name.levis.talosmobile.monitor.NodeState
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class WidgetStatusTest {

    private val now = 1_800_000_000_000L

    private fun snap(
        vararg health: NodeHealth,
        alarms: Int = 0,
        etcdChecked: Boolean = true,
        takenAt: Long = now,
    ) = ClusterSnapshot(
        context = "lab",
        takenAt = takenAt,
        nodes = health.withIndex().associate { (i, h) -> "192.0.2.$i" to NodeState("n$i", h) },
        etcdAlarms = List(alarms) { "NOSPACE" },
        etcdChecked = etcdChecked,
    )

    @Test
    fun allReadyThenEtcd() {
        assertEquals(
            listOf(StatusItem.AllReady, StatusItem.Etcd(0)),
            statusItems(snap(READY, READY)),
        )
    }

    @Test
    fun etcdHiddenWhenNotChecked() {
        assertEquals(listOf(StatusItem.AllReady), statusItems(snap(READY, etcdChecked = false)))
    }

    @Test
    fun problemsComeFirstAndCapAtTwo() {
        assertEquals(
            listOf(StatusItem.NotReady(2), StatusItem.Unreachable(1)),
            statusItems(snap(READY, NOT_READY, NOT_READY, UNREACHABLE, alarms = 3)),
        )
    }

    @Test
    fun oneProblemLeavesRoomForEtcdAlarms() {
        assertEquals(
            listOf(StatusItem.Unreachable(1), StatusItem.Etcd(2)),
            statusItems(snap(READY, UNREACHABLE, alarms = 2)),
        )
    }

    @Test
    fun noNodesIsNotAllReady() {
        assertEquals(emptyList<StatusItem>(), statusItems(snap(etcdChecked = false)))
    }

    @Test
    fun staleness() {
        assertTrue(isStale(null, now))
        assertFalse(isStale(snap(READY), now))
        assertFalse(isStale(snap(READY, takenAt = now - WIDGET_STALE_AFTER_MS), now))
        assertTrue(isStale(snap(READY, takenAt = now - WIDGET_STALE_AFTER_MS - 1), now))
    }
}
