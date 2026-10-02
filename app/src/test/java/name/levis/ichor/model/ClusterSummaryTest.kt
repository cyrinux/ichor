package name.levis.ichor.model

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class ClusterSummaryTest {

    private val gib = 1L shl 30

    private fun node(
        addr: String,
        ready: Boolean = true,
        reachable: Boolean = true,
        version: String = "v1.11.2",
        cpus: Int = 4,
        mem: Long = 8 * gib,
        free: Long = 4 * gib,
    ) = NodeOverview(
        node = addr, hostname = addr, reachable = reachable, ready = ready, version = version,
        cpuCount = cpus, memTotal = mem, memAvailable = free,
    )

    @Test
    fun allReadyIsHealthyWithSummedCapacity() {
        val s = clusterSummary(listOf(node("a"), node("b", cpus = 8, mem = 16 * gib, free = 2 * gib)))
        assertEquals(ClusterStatus.HEALTHY, s.status)
        assertEquals(2, s.total)
        assertEquals(2, s.ready)
        assertEquals(12, s.cpuCount)
        assertEquals(24 * gib, s.memTotal)
        assertEquals(6 * gib, s.memAvailable)
        assertEquals(listOf("v1.11.2"), s.versions)
    }

    @Test
    fun anyNodeNotReadyOrUnreachableIsDegraded() {
        val s = clusterSummary(listOf(node("a"), node("b", ready = false), node("c", reachable = false, cpus = 0, mem = 0, free = 0)))
        assertEquals(ClusterStatus.DEGRADED, s.status)
        assertEquals(1, s.ready)
        assertEquals(1, s.notReady)
        assertEquals(1, s.unreachable)
        assertEquals(8, s.cpuCount)
    }

    @Test
    fun noReachableNodeIsDown() {
        val s = clusterSummary(listOf(node("a", reachable = false), node("b", reachable = false)))
        assertEquals(ClusterStatus.DOWN, s.status)
        // An unreachable node's last answer must not count.
        assertEquals(0, s.cpuCount)
        assertEquals(0L, s.memTotal)
    }

    @Test
    fun noNodesIsDown() {
        assertEquals(ClusterStatus.DOWN, clusterSummary(emptyList()).status)
    }

    @Test
    fun versionsAreDistinctAndOrderedNumerically() {
        val s = clusterSummary(listOf(node("a", version = "v1.10.0"), node("b", version = "v1.9.5"), node("c", version = "v1.10.0")))
        assertEquals(listOf("v1.9.5", "v1.10.0"), s.versions)
    }

    @Test
    fun memoryFractionIsNullWhenUnknown() {
        assertNull(clusterSummary(listOf(node("a", mem = 0, free = 0))).memUsedFraction)
        assertEquals(0.75f, clusterSummary(listOf(node("a", mem = 8 * gib, free = 2 * gib))).memUsedFraction!!, 0.001f)
    }
}
