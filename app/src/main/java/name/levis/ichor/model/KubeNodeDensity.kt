package name.levis.ichor.model

/*
 * The Kubernetes home's nodes, the way the Talos home handles a large cluster (NodeDensity):
 * a status per node, counts, the problem nodes capped, and the search and filter of the
 * Kubernetes nodes screen. A kube node is never NodeStatus.UNREACHABLE: Kubernetes only says
 * ready or not.
 */

/** Not ready; else ready but cordoned or under pressure ([NodeStatus.ATTENTION]); else ready. */
val KubeNodeInfo.status: NodeStatus
    get() = when {
        !ready -> NodeStatus.NOT_READY
        cordoned || pressure.isNotEmpty() -> NodeStatus.ATTENTION
        else -> NodeStatus.READY
    }

/** Not ready, cordoned or under pressure: listed first, tinted. */
val KubeNodeInfo.needsAttention: Boolean get() = status != NodeStatus.READY

fun List<KubeNodeInfo>.kubeHealthCounts(): HealthCounts = healthCounts { it.status }

/** The order the dense card and the nodes screen group by: the worst first. */
val KUBE_STATUS_ORDER = listOf(NodeStatus.NOT_READY, NodeStatus.ATTENTION, NodeStatus.READY)

/** The nodes of one [status], in the core's order (control planes first, then by name). */
data class KubeStatusGroup(val status: NodeStatus, val nodes: List<KubeNodeInfo>)

/** [this] grouped by status, worst group first ([KUBE_STATUS_ORDER]); empty groups left out. */
fun List<KubeNodeInfo>.byStatus(): List<KubeStatusGroup> {
    val groups = groupBy { it.status }
    return KUBE_STATUS_ORDER.mapNotNull { status -> groups[status]?.let { KubeStatusGroup(status, it) } }
}

/** Worst first: not ready, then cordoned or under pressure ([KUBE_STATUS_ORDER]). */
fun List<KubeNodeInfo>.kubeProblemNodes(max: Int = DENSE_MAX_PROBLEMS): ProblemNodes<KubeNodeInfo> =
    problemNodes(max, { it.needsAttention }, { KUBE_STATUS_ORDER.indexOf(it.status) })

/** The filters the Kubernetes nodes screen offers: no node is unreachable to Kubernetes. */
val KUBE_NODE_FILTERS = listOf(NodeFilter.ATTENTION, NodeFilter.READY, NodeFilter.NOT_READY)

fun NodeFilter.matches(node: KubeNodeInfo): Boolean = when (this) {
    NodeFilter.ATTENTION -> node.needsAttention
    NodeFilter.READY -> node.ready
    NodeFilter.NOT_READY -> !node.ready
    NodeFilter.UNREACHABLE -> false
}

/**
 * [this] narrowed to the nodes whose name or address (internal or external) contains [query],
 * matching [filter] (null: any).
 */
fun List<KubeNodeInfo>.filterKubeNodes(query: String, filter: NodeFilter?): List<KubeNodeInfo> {
    val q = query.trim()
    return filter { node -> (filter == null || filter.matches(node)) && (q.isEmpty() || node.matchesQuery(q)) }
}

private fun KubeNodeInfo.matchesQuery(query: String): Boolean =
    name.contains(query, ignoreCase = true) ||
        internalIP.contains(query, ignoreCase = true) ||
        externalIP.contains(query, ignoreCase = true)
