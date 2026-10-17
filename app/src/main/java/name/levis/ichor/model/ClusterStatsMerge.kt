package name.levis.ichor.model

/**
 * On a dense cluster, every [FULL_SAMPLE_EVERY]th sample asks every node; the others only the
 * focus set (problem nodes and the rows on screen).
 */
const val FULL_SAMPLE_EVERY = 4

/**
 * Ticks further apart than this (the screen was left a while) start the merge afresh: twice
 * the dense period, as a sample takes a few seconds on top of it.
 */
const val DENSE_MAX_TICK_GAP_MILLIS = 2 * DENSE_CLUSTER_POLL_SECONDS * 1000

/**
 * A node not read for this long is dropped (it left the cluster or stopped answering), and its
 * next reading makes no CPU delta: a little over two full-sample rounds.
 */
const val DENSE_MAX_NODE_GAP_MILLIS = 2 * FULL_SAMPLE_EVERY * DENSE_CLUSTER_POLL_SECONDS * 1000 + DENSE_MAX_TICK_GAP_MILLIS

/**
 * A node's latest counters, read at [at], and the CPU time it spent busy ([busy]) out of
 * [total] between its last two readings, however far apart they were; null until it has two
 * (or after it rebooted).
 */
data class NodeReading(val counters: NodeCounters, val at: Long, val busy: Double? = null, val total: Double? = null)

/**
 * The last known counters of every node across partial samples, so the cluster total covers
 * every node, not only the ones of the latest sample. [sampled] of [total] nodes were asked last.
 */
data class MergedClusterStats(
    val at: Long = 0,
    val nodes: Map<String, NodeReading> = emptyMap(),
    val sampled: Int = 0,
    val total: Int = 0,
) {
    /** Whether the last sample asked fewer nodes than the cluster has. */
    val partial: Boolean get() = sampled in 1 until total
}

/**
 * [prev] updated with [cur]: each node in [cur] gets its new counters and the CPU delta since
 * its own previous reading (CPU is a delta per node, so a node read at t0 and again at t4 still
 * gives a true average); the others keep their last counters until [maxNodeGap] old. A full
 * sample (every node asked) drops the nodes that did not answer, as [clusterUsage] does.
 */
fun mergeClusterStats(
    prev: MergedClusterStats?,
    cur: ClusterStatsSample,
    maxTickGap: Long = DENSE_MAX_TICK_GAP_MILLIS,
    maxNodeGap: Long = DENSE_MAX_NODE_GAP_MILLIS,
): MergedClusterStats {
    val before = prev?.takeIf { cur.at - it.at <= maxTickGap }?.nodes.orEmpty()
    val read = cur.nodes.associate { n -> n.node to reading(before[n.node], n, cur.at, maxNodeGap) }
    val full = cur.total <= 0 || cur.sampled >= cur.total
    val kept = if (full) emptyMap() else before.filter { (node, r) -> node !in read && cur.at - r.at <= maxNodeGap }
    return MergedClusterStats(at = cur.at, nodes = kept + read, sampled = cur.sampled, total = cur.total)
}

private fun reading(prev: NodeReading?, cur: NodeCounters, at: Long, maxNodeGap: Long): NodeReading {
    val p = prev?.takeIf { at - it.at <= maxNodeGap } ?: return NodeReading(cur, at)
    val busy = cur.cpuBusy - p.counters.cpuBusy
    val total = cur.cpuTotal - p.counters.cpuTotal
    // Counters that went backwards: the node rebooted, no delta until its next reading.
    return if (busy < 0 || total <= 0) NodeReading(cur, at) else NodeReading(cur, at, busy, total)
}

/**
 * Usage over every node's latest reading: CPU sums each node's last busy and total deltas, so
 * a big node weighs more than a small one; memory sums the last known memory of every node,
 * 0 when one of them did not say, so it is not understated.
 */
fun MergedClusterStats.usage(): ClusterUsage {
    val readings = nodes.values
    val deltas = readings.mapNotNull { r -> r.busy?.let { b -> r.total?.let { t -> b to t } } }
    val total = deltas.sumOf { it.second }
    val cpu = if (total > 0) (deltas.sumOf { it.first } / total).toFloat().coerceIn(0f, 1f) else null
    val memoryComplete = readings.isNotEmpty() && readings.all { it.counters.memTotal > 0 }
    return ClusterUsage(
        cpuFraction = cpu,
        memTotal = if (memoryComplete) readings.sumOf { it.counters.memTotal } else 0,
        memAvailable = if (memoryComplete) readings.sumOf { it.counters.memAvailable } else 0,
        nodes = readings.size,
    )
}

/**
 * The nodes to ask on sample [tick] (0 first) of a cluster of [nodeCount] nodes: every node
 * (empty) below the dense threshold, on the first two ticks (the first CPU delta), every
 * [FULL_SAMPLE_EVERY]th tick and while [focus] is empty; otherwise the [focus] set, sorted.
 */
fun liveSampleNodes(tick: Int, nodeCount: Int, focus: Set<String>): List<String> {
    val full = !isDenseCluster(nodeCount) || tick < 2 || tick % FULL_SAMPLE_EVERY == 0 || focus.isEmpty()
    return if (full) emptyList() else focus.sorted()
}
