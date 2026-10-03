package name.levis.ichor.model

/**
 * This overview with what [previous] (fetched at [previousAt], epoch millis) knew of the nodes
 * that no longer answer: an unreachable node comes back from the core named by its address,
 * with no role or version. Each one that answered before keeps its hostname, role, version and
 * capacity, and gets [NodeOverview.lastSeen]; its health, error and live figures stay the new
 * ones. Nodes that answer are left as they are.
 */
fun ClusterOverview.withLastKnown(previous: ClusterOverview?, previousAt: Long): ClusterOverview {
    if (previous == null) return this
    val known = previous.nodes.associateBy { it.node }
    return copy(nodes = nodes.map { node -> known[node.node]?.let { node.lastKnownFrom(it, previousAt) } ?: node })
}

private fun NodeOverview.lastKnownFrom(before: NodeOverview, beforeAt: Long): NodeOverview {
    if (reachable) return this
    val seen = before.lastSeen ?: beforeAt.takeIf { before.reachable } ?: return this
    return copy(
        hostname = before.hostname,
        version = before.version,
        arch = before.arch,
        platform = before.platform,
        role = before.role,
        cpuCount = before.cpuCount,
        memTotal = before.memTotal,
        lastSeen = seen,
    )
}

/** Whether the overview has anything to show of nodes that no longer answer. */
val ClusterOverview.hasLastKnown: Boolean get() = nodes.any { it.lastSeen != null }
