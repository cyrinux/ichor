package name.levis.ichor.model

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class KubeClusterSummaryTest {
    private val gib = 1L shl 30
    private val nodes = KubeNodesOverview(
        nodes = listOf(
            KubeNodeInfo(name = "worker-1", ready = true, cpu = 4.0, memory = 16.0 * gib),
            KubeNodeInfo(name = "worker-2", ready = true, cpu = 8.0, memory = 8.0 * gib),
            KubeNodeInfo(name = "worker-3", ready = false, cpu = 4.0, memory = 8.0 * gib),
        ),
    )

    @Test
    fun countsTheNodesAndTheirCapacity() {
        val summary = nodes.clusterSummary(top = null)
        assertEquals(3, summary.total)
        assertEquals(2, summary.ready)
        assertEquals(1, summary.notReady)
        assertEquals(ClusterStatus.DEGRADED, summary.status)
        assertEquals(16, summary.cpuCount)
        assertEquals(32 * gib, summary.memTotal)
        // No metrics-server: no share in use, never a misleading 0 %.
        assertNull(summary.cpuFraction)
        assertNull(summary.memFraction)
    }

    @Test
    fun theShareInUseIsOfTheAllocatableCapacity() {
        val top = KubeTopNodes(
            available = true,
            nodes = listOf(
                KubeTopNode(name = "worker-1", cpu = 1.0, cpuAllocatable = 4.0, memory = 4.0 * gib, memoryAllocatable = 15.0 * gib),
                KubeTopNode(name = "worker-2", cpu = 3.0, cpuAllocatable = 8.0, memory = 2.0 * gib, memoryAllocatable = 7.5 * gib),
            ),
        )
        val summary = nodes.clusterSummary(top)
        assertEquals(4f / 12f, summary.cpuFraction!!, 1e-6f)
        assertEquals(6f / 22.5f, summary.memFraction!!, 1e-6f)
    }

    @Test
    fun everyNodeReadyIsHealthyAndNoneIsDown() {
        val ready = nodes.copy(nodes = nodes.nodes.map { it.copy(ready = true) })
        assertEquals(ClusterStatus.HEALTHY, ready.clusterSummary(null).status)
        val down = nodes.copy(nodes = nodes.nodes.map { it.copy(ready = false) })
        assertEquals(ClusterStatus.DOWN, down.clusterSummary(null).status)
    }
}
