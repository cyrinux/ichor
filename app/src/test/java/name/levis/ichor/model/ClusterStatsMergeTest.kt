package name.levis.ichor.model

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class ClusterStatsMergeTest {

    private val gib = 1L shl 30

    private fun counters(node: String, busy: Double, total: Double, mem: Long = 8 * gib, free: Long = 4 * gib) =
        NodeCounters(node = node, cpuBusy = busy, cpuTotal = total, cpuCount = 4, memTotal = mem, memAvailable = free)

    private fun sample(at: Long, total: Int, vararg nodes: NodeCounters, sampled: Int = nodes.size) =
        ClusterStatsSample(at, nodes.toList(), sampled = sampled, total = total)

    private val tick = DENSE_CLUSTER_POLL_SECONDS * 1000

    @Test
    fun aPartialSampleKeepsTheOtherNodesLastCounters() {
        val t0 = mergeClusterStats(null, sample(0, 3, counters("a", 0.0, 0.0), counters("b", 0.0, 0.0), counters("c", 0.0, 0.0)))
        val t1 = mergeClusterStats(t0, sample(tick, 3, counters("a", 10.0, 100.0), counters("b", 30.0, 100.0), counters("c", 50.0, 100.0)))
        // Only "a" is in focus: "b" and "c" keep their last deltas, so the total covers all three.
        val t2 = mergeClusterStats(t1, sample(2 * tick, 3, counters("a", 110.0, 200.0)))
        val usage = t2.usage()
        assertEquals(3, usage.nodes)
        assertEquals((100.0 + 30 + 50).toFloat() / 300, usage.cpuFraction!!, 1e-6f)
        assertEquals(24 * gib, usage.memTotal)
        assertTrue(t2.partial)
    }

    @Test
    fun aNodeSampledAgainAfterAGapGivesItsAverageOverTheGap() {
        val nodes = listOf("a", "b")
        val t0 = mergeClusterStats(null, sample(0, 2, counters("a", 0.0, 0.0), counters("b", 0.0, 0.0)))
        // "b" is only in the full samples: read at t0 and again at t4, 60 s later.
        var merged = t0
        for (t in 1..3) merged = mergeClusterStats(merged, sample(t * tick, 2, counters("a", t * 10.0, t * 100.0)))
        merged = mergeClusterStats(merged, sample(4 * tick, 2, counters("a", 40.0, 400.0), counters("b", 200.0, 400.0)))
        assertEquals(nodes.toSet(), merged.nodes.keys)
        assertEquals(200.0, merged.nodes.getValue("b").busy!!, 1e-9)
        assertEquals(400.0, merged.nodes.getValue("b").total!!, 1e-9)
        // a's last delta (10 of 100) plus b's over the whole gap (200 of 400).
        assertEquals(210f / 500, merged.usage().cpuFraction!!, 1e-6f)
        assertFalse(merged.partial)
    }

    @Test
    fun aFullSampleDropsTheNodesThatDidNotAnswer() {
        val t0 = mergeClusterStats(null, sample(0, 2, counters("a", 0.0, 0.0), counters("gone", 0.0, 0.0)))
        val t1 = mergeClusterStats(t0, sample(tick, 2, counters("a", 1.0, 2.0), sampled = 2))
        assertEquals(setOf("a"), t1.nodes.keys)
    }

    @Test
    fun aNodeNotReadForTooLongIsDropped() {
        var merged = mergeClusterStats(null, sample(0, 2, counters("a", 0.0, 0.0), counters("b", 0.0, 0.0)))
        var at = 0L
        while (at <= DENSE_MAX_NODE_GAP_MILLIS) {
            at += tick
            merged = mergeClusterStats(merged, sample(at, 2, counters("a", at.toDouble(), at * 2.0)))
        }
        assertEquals(setOf("a"), merged.nodes.keys)
    }

    @Test
    fun leavingTheScreenStartsAfresh() {
        val t0 = mergeClusterStats(null, sample(0, 2, counters("a", 0.0, 0.0), counters("b", 0.0, 0.0)))
        val later = mergeClusterStats(t0, sample(DENSE_MAX_TICK_GAP_MILLIS + 1, 2, counters("a", 50.0, 100.0)))
        assertEquals(setOf("a"), later.nodes.keys)
        assertNull(later.usage().cpuFraction)
    }

    @Test
    fun aRebootedNodeHasNoDeltaUntilItsNextReading() {
        val t0 = mergeClusterStats(null, sample(0, 1, counters("a", 5_000.0, 9_000.0)))
        val t1 = mergeClusterStats(t0, sample(tick, 1, counters("a", 1.0, 10.0)))
        assertNull(t1.usage().cpuFraction)
        val t2 = mergeClusterStats(t1, sample(2 * tick, 1, counters("a", 6.0, 20.0)))
        assertEquals(0.5f, t2.usage().cpuFraction!!, 1e-6f)
    }

    @Test
    fun aNodeWithoutMemoryDropsTheMemory() {
        val merged = mergeClusterStats(null, sample(0, 2, counters("a", 0.0, 0.0), counters("b", 0.0, 0.0, mem = 0, free = 0)))
        assertNull(merged.usage().memUsedFraction)
    }

    @Test
    fun smallClustersAlwaysAskEveryNode() {
        val focus = setOf("a")
        (0..8).forEach { assertEquals(emptyList<String>(), liveSampleNodes(it, NODE_DENSE_THRESHOLD, focus)) }
    }

    @Test
    fun denseClustersAskTheFocusSetBetweenFullSamples() {
        val n = NODE_DENSE_THRESHOLD + 1
        val focus = setOf("worker-9", "worker-2")
        val asked = (0..8).map { liveSampleNodes(it, n, focus) }
        val full = asked.indices.filter { asked[it].isEmpty() }
        // The first two make the first CPU delta of every node; then one in FULL_SAMPLE_EVERY.
        assertEquals(listOf(0, 1, 4, 8), full)
        assertEquals(listOf("worker-2", "worker-9"), asked[2])
        assertEquals(emptyList<String>(), liveSampleNodes(2, n, emptySet()))
    }
}
