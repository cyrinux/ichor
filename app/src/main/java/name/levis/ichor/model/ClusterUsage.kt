package name.levis.ichor.model

import kotlinx.serialization.Serializable
import name.levis.ichor.util.usedFraction

/** Cumulative CPU times and current memory of one node, from the Go core (ClusterStats). */
@Serializable
data class NodeCounters(
    val node: String,
    val cpuBusy: Double,
    val cpuTotal: Double,
    val cpuCount: Int = 0,
    val memTotal: Long = 0,
    val memAvailable: Long = 0,
)

/** One sample of every node that answered. */
@Serializable
data class ClusterStatsSample(val at: Long, val nodes: List<NodeCounters> = emptyList())

/**
 * Samples further apart than this (the screen was left a while) do not make a CPU point: it
 * would average minutes into what the sparkline draws as one step.
 */
const val MAX_SAMPLE_GAP_MILLIS = 15_000L

/** Live usage of the cluster for the overview card. */
data class ClusterUsage(
    /** Share of all CPU time spent busy since the previous sample; null until there are two. */
    val cpuFraction: Float?,
    /** Total memory of the answering nodes; 0 when one of them did not say, so it is not understated. */
    val memTotal: Long,
    val memAvailable: Long,
    /** Nodes that answered the sample. */
    val nodes: Int,
) {
    val memUsedFraction: Float?
        get() = if (memTotal <= 0) null else usedFraction(memTotal, memAvailable)
}

/**
 * Usage between two samples. CPU sums the busy and total time deltas of every node present in
 * both, so a big node weighs more than a small one; a node whose counters went backwards
 * (it rebooted) is skipped. Memory is the latest sample's.
 */
fun clusterUsage(prev: ClusterStatsSample?, cur: ClusterStatsSample): ClusterUsage {
    val before = prev?.takeIf { cur.at - it.at <= MAX_SAMPLE_GAP_MILLIS }?.nodes?.associateBy { it.node }.orEmpty()
    val deltas = cur.nodes.mapNotNull { n ->
        val p = before[n.node] ?: return@mapNotNull null
        val busy = n.cpuBusy - p.cpuBusy
        val total = n.cpuTotal - p.cpuTotal
        if (busy < 0 || total <= 0) null else busy to total
    }
    val total = deltas.sumOf { it.second }
    val cpu = if (total > 0) (deltas.sumOf { it.first } / total).toFloat().coerceIn(0f, 1f) else null
    val memoryComplete = cur.nodes.isNotEmpty() && cur.nodes.all { it.memTotal > 0 }
    return ClusterUsage(
        cpuFraction = cpu,
        memTotal = if (memoryComplete) cur.nodes.sumOf { it.memTotal } else 0,
        memAvailable = if (memoryComplete) cur.nodes.sumOf { it.memAvailable } else 0,
        nodes = cur.nodes.size,
    )
}

/** [history] with [value] appended (nothing when null), keeping the newest [max] points. */
fun appendHistory(history: List<Float>, value: Float?, max: Int): List<Float> =
    if (value == null) history else (history + value).takeLast(max)
