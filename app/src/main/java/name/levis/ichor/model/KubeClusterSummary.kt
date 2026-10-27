package name.levis.ichor.model

/**
 * What the Kubernetes home's summary card shows, as the Talos one: the node counts, an overall
 * status, the capacity, and with metrics-server ([KubeTopNodes]) the share of it in use.
 */
data class KubeClusterSummary(
    val total: Int,
    val ready: Int,
    val status: ClusterStatus,
    /** Cores of every node, rounded; 0 when no node said. */
    val cpuCount: Int,
    /** Bytes of every node; 0 when no node said. */
    val memTotal: Long,
    /** Busy share of the allocatable CPU, null without metrics-server. */
    val cpuFraction: Float?,
    /** Used share of the allocatable memory, null without metrics-server. */
    val memFraction: Float?,
) {
    val notReady: Int get() = total - ready
}

/** The summary of [this] list of nodes, with [top] usage when metrics-server answered. */
fun KubeNodesOverview.clusterSummary(top: KubeTopNodes?): KubeClusterSummary {
    val ready = readyCount
    val total = nodes.size
    val used = top?.takeIf { it.available }?.nodes.orEmpty()
    return KubeClusterSummary(
        total = total,
        ready = ready,
        status = when (ready) {
            total -> ClusterStatus.HEALTHY
            0 -> ClusterStatus.DOWN
            else -> ClusterStatus.DEGRADED
        },
        cpuCount = Math.round(nodes.sumOf { it.cpu }).toInt(),
        memTotal = nodes.sumOf { it.memory }.toLong(),
        cpuFraction = fraction(used.sumOf { it.cpu }, used.sumOf { it.cpuAllocatable }),
        memFraction = fraction(used.sumOf { it.memory }, used.sumOf { it.memoryAllocatable }),
    )
}

private fun fraction(used: Double, of: Double): Float? = if (of > 0) (used / of).coerceIn(0.0, 1.0).toFloat() else null
