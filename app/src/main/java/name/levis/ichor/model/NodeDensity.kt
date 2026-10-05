package name.levis.ichor.model

/**
 * Above this many nodes, the home Nodes card turns dense: a dot per node instead of a chip with
 * its hostname, problems capped, and "expand" opens the Nodes screen instead of growing inline.
 */
const val NODE_DENSE_THRESHOLD = 24

/** Problem nodes the dense card lists in full; the others are behind "N more". */
const val DENSE_MAX_PROBLEMS = 5

/** Seconds between live cluster samples: live enough to read, light on the nodes and the phone's data. */
const val CLUSTER_POLL_SECONDS = 5L

/** The same on a dense cluster: every sample asks every node, so fewer of them. */
const val DENSE_CLUSTER_POLL_SECONDS = 15L

/** Whether a cluster of [nodeCount] nodes gets the dense home layout. */
fun isDenseCluster(nodeCount: Int): Boolean = nodeCount > NODE_DENSE_THRESHOLD

/** Seconds between live cluster samples for a cluster of [nodeCount] nodes. */
fun clusterPollSeconds(nodeCount: Int): Long = if (isDenseCluster(nodeCount)) DENSE_CLUSTER_POLL_SECONDS else CLUSTER_POLL_SECONDS

/** How many nodes are in each [NodeHealth]. */
data class HealthCounts(val ready: Int = 0, val notReady: Int = 0, val unreachable: Int = 0) {
    val total: Int get() = ready + notReady + unreachable

    operator fun get(health: NodeHealth): Int = when (health) {
        NodeHealth.READY -> ready
        NodeHealth.NOT_READY -> notReady
        NodeHealth.UNREACHABLE -> unreachable
    }
}

fun List<NodeOverview>.healthCounts(): HealthCounts {
    val byHealth = groupingBy { it.health }.eachCount()
    return HealthCounts(
        ready = byHealth[NodeHealth.READY] ?: 0,
        notReady = byHealth[NodeHealth.NOT_READY] ?: 0,
        unreachable = byHealth[NodeHealth.UNREACHABLE] ?: 0,
    )
}

/** The problem nodes the dense card shows in full ([shown]), and how many more there are ([more]). */
data class ProblemNodes(val shown: List<NodeOverview>, val more: Int)

/**
 * The nodes [needsAttention], worst first (unreachable, not ready, then ready but reporting a
 * problem), in their given order otherwise; at most [max] of them, the rest counted.
 */
fun List<NodeOverview>.problemNodes(max: Int = DENSE_MAX_PROBLEMS): ProblemNodes {
    val problems = filter { it.needsAttention }.sortedBy { severity(it.health) }
    return ProblemNodes(problems.take(max), (problems.size - max).coerceAtLeast(0))
}

private fun severity(health: NodeHealth) = when (health) {
    NodeHealth.UNREACHABLE -> 0
    NodeHealth.NOT_READY -> 1
    NodeHealth.READY -> 2
}

/** What the Nodes screen narrows its list to, besides the search. */
enum class NodeFilter {
    /** Down, not ready, or reporting a problem: the dense card's "N more". */
    ATTENTION,
    READY,
    NOT_READY,
    UNREACHABLE,
    ;

    fun matches(node: NodeOverview): Boolean = when (this) {
        ATTENTION -> node.needsAttention
        READY -> node.health == NodeHealth.READY
        NOT_READY -> node.health == NodeHealth.NOT_READY
        UNREACHABLE -> node.health == NodeHealth.UNREACHABLE
    }
}

/** A key per group for the site filter: the site's id, empty for the nodes the map misses. */
val NodeGroup.key: String get() = site?.id.orEmpty()

/**
 * [this] groups narrowed to the nodes whose hostname or address (talosconfig or public) contains
 * [query], matching [filter] (null: any), on the site keyed [site] (null: any); groups left
 * empty are dropped.
 */
fun List<NodeGroup>.filterNodes(query: String, filter: NodeFilter?, site: String?): List<NodeGroup> {
    val q = query.trim()
    return filter { site == null || it.key == site }
        .map { group ->
            group.copy(
                nodes = group.nodes.filter { node ->
                    (filter == null || filter.matches(node)) && (q.isEmpty() || node.matchesQuery(q))
                },
            )
        }
        .filter { it.nodes.isNotEmpty() }
}

private fun NodeOverview.matchesQuery(query: String): Boolean =
    hostname.contains(query, ignoreCase = true) ||
        node.contains(query, ignoreCase = true) ||
        publicIPs.any { it.contains(query, ignoreCase = true) }
