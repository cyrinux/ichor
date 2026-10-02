package name.levis.ichor.model

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class ClusterUsageTest {

    private val gib = 1L shl 30

    private fun counters(node: String, busy: Double, total: Double, mem: Long = 8 * gib, free: Long = 4 * gib) =
        NodeCounters(node = node, cpuBusy = busy, cpuTotal = total, cpuCount = 4, memTotal = mem, memAvailable = free)

    private fun sample(at: Long, vararg nodes: NodeCounters) = ClusterStatsSample(at, nodes.toList())

    @Test
    fun cpuIsBusyOverTotalTimeAcrossNodes() {
        val prev = sample(0, counters("a", 0.0, 0.0), counters("b", 0.0, 0.0))
        val cur = sample(5_000, counters("a", 10.0, 100.0), counters("b", 90.0, 300.0))
        // Bigger nodes weigh more: (10 + 90) / (100 + 300), not the mean of 10% and 30%.
        assertEquals(0.25f, clusterUsage(prev, cur).cpuFraction!!, 1e-6f)
    }

    @Test
    fun memoryComesFromTheLatestSample() {
        val prev = sample(0, counters("a", 0.0, 0.0))
        val cur = sample(5_000, counters("a", 1.0, 2.0, mem = 16 * gib, free = 4 * gib))
        val usage = clusterUsage(prev, cur)
        assertEquals(16 * gib, usage.memTotal)
        assertEquals(4 * gib, usage.memAvailable)
        assertEquals(0.75f, usage.memUsedFraction!!, 1e-6f)
    }

    @Test
    fun nodesMissingFromEitherSampleDoNotCountForCpu() {
        val prev = sample(0, counters("a", 0.0, 0.0), counters("gone", 0.0, 0.0))
        val cur = sample(5_000, counters("a", 50.0, 100.0), counters("new", 1_000.0, 1_000.0))
        assertEquals(0.5f, clusterUsage(prev, cur).cpuFraction!!, 1e-6f)
    }

    @Test
    fun rebootedNodeIsSkippedRatherThanSkewingTheAverage() {
        val prev = sample(0, counters("a", 0.0, 0.0), counters("rebooted", 5_000.0, 9_000.0))
        val cur = sample(5_000, counters("a", 20.0, 100.0), counters("rebooted", 1.0, 10.0))
        assertEquals(0.2f, clusterUsage(prev, cur).cpuFraction!!, 1e-6f)
    }

    @Test
    fun noElapsedCpuTimeGivesNoCpu() {
        val same = sample(0, counters("a", 10.0, 100.0))
        assertNull(clusterUsage(same, same).cpuFraction)
    }

    @Test
    fun samplesTooFarApartGiveNoCpu() {
        val prev = sample(0, counters("a", 0.0, 0.0))
        val cur = sample(MAX_SAMPLE_GAP_MILLIS + 1, counters("a", 50.0, 100.0))
        assertNull(clusterUsage(prev, cur).cpuFraction)
    }

    @Test
    fun aNodeWithoutMemoryDropsTheMemoryRatherThanUnderstateIt() {
        val cur = sample(0, counters("a", 1.0, 2.0), counters("b", 1.0, 2.0, mem = 0, free = 0))
        val usage = clusterUsage(null, cur)
        assertEquals(2, usage.nodes)
        assertNull(usage.memUsedFraction)
    }

    @Test
    fun noMemoryReportedGivesNoMemoryFraction() {
        val prev = sample(0, counters("a", 0.0, 0.0, mem = 0, free = 0))
        val cur = sample(5_000, counters("a", 1.0, 2.0, mem = 0, free = 0))
        assertNull(clusterUsage(prev, cur).memUsedFraction)
    }

    @Test
    fun firstSampleAloneHasMemoryButNoCpu() {
        val usage = clusterUsage(null, sample(0, counters("a", 10.0, 100.0)))
        assertNull(usage.cpuFraction)
        assertEquals(0.5f, usage.memUsedFraction!!, 1e-6f)
    }

    @Test
    fun historyKeepsTheNewestPoints() {
        assertEquals(listOf(0.2f, 0.3f), appendHistory(listOf(0.1f, 0.2f), 0.3f, max = 2))
        assertEquals(listOf(0.1f), appendHistory(listOf(0.1f), null, max = 2))
    }
}
