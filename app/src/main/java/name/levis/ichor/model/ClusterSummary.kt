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
    /** The KubeSpan links were read: false without KubeSpan or before the map landed. */
    val meshChecked: Boolean = false,
    val meshBroken: Int = 0,
    /** "node-a ↔ node-b" of the broken links, worst first. */
    val meshPairs: List<String> = emptyList(),
) {
    val status: ClusterStatus
        get() = when {
            total == 0 || unreachable == total -> ClusterStatus.DOWN
            // A tunnel down is a degraded cluster, never a down one: its nodes still answer.
            ready == total && meshBroken == 0 -> ClusterStatus.HEALTHY
            else -> ClusterStatus.DEGRADED
        }

    /** Memory in use across the cluster, null when no node said how much it has. */
    val memUsedFraction: Float?
        get() = if (memTotal <= 0) null else usedFraction(memTotal, memAvailable)
}

fun clusterSummary(nodes: List<NodeOverview>, topology: ClusterTopology? = null): ClusterSummary {
    val mesh = topology?.mesh
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
        meshChecked = mesh?.checked == true,
        meshBroken = mesh?.broken ?: 0,
        meshPairs = mesh?.pairs.orEmpty(),
    )
}
