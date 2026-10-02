package name.levis.ichor.model

import name.levis.ichor.util.usedFraction

/** The cluster as a whole: every node ready, some trouble, or nothing answering. */
enum class ClusterStatus { HEALTHY, DEGRADED, DOWN }

/** Headline numbers of the overview card; capacity only counts the nodes that answered. */
data class ClusterSummary(
    val total: Int,
    val ready: Int,
    val notReady: Int,
    val unreachable: Int,
    /** Distinct Talos versions, oldest first: more than one means an upgrade in progress. */
    val versions: List<String>,
    val cpuCount: Int,
    val memTotal: Long,
    val memAvailable: Long,
) {
    val status: ClusterStatus
        get() = when {
            total == 0 || unreachable == total -> ClusterStatus.DOWN
            ready == total -> ClusterStatus.HEALTHY
            else -> ClusterStatus.DEGRADED
        }

    /** Memory in use across the cluster, null when no node said how much it has. */
    val memUsedFraction: Float?
        get() = if (memTotal <= 0) null else usedFraction(memTotal, memAvailable)
}

fun clusterSummary(nodes: List<NodeOverview>): ClusterSummary {
    val counts = nodes.groupingBy { it.health }.eachCount()
    val reachable = nodes.filter { it.reachable }
    return ClusterSummary(
        total = nodes.size,
        ready = counts[NodeHealth.READY] ?: 0,
        notReady = counts[NodeHealth.NOT_READY] ?: 0,
        unreachable = counts[NodeHealth.UNREACHABLE] ?: 0,
        versions = reachable.map { it.version }.filter { it.isNotBlank() }.distinct().sortedWith(::compareVersions),
        cpuCount = reachable.sumOf { it.cpuCount },
        memTotal = reachable.sumOf { it.memTotal },
        memAvailable = reachable.sumOf { it.memAvailable },
    )
}
