package name.levis.ichor.model

import kotlinx.coroutines.flow.Flow

/** One step of a streamed overview (Go StartClusterOverview). */
sealed interface OverviewPart {
    /** A node that answered or failed, in completion order; lacks the peer-seen public IPs. */
    data class Node(val node: NodeOverview) : OverviewPart

    /** The complete overview, exactly as ClusterOverview returns it. */
    data class Done(val overview: ClusterOverview) : OverviewPart
}

/**
 * The nodes the overview asks, as the core picks them (targetNodes): the context's nodes, else
 * its endpoints; none known for an Omni context, whose nodes come from Omni.
 */
fun ContextSummary.overviewTargets(): List<String> = when {
    nodes.isNotEmpty() -> nodes.distinct()
    omni -> emptyList()
    else -> endpoints.distinct()
}

/**
 * The [answered] nodes so far as a [ClusterOverview.partial] overview of [context]: in the
 * order of [targets] (the order of the complete one, so rows do not jump when it comes), any
 * node not in it after them in arrival order.
 */
fun partialOverview(context: String, answered: List<NodeOverview>, targets: List<String>): ClusterOverview {
    val rank = targets.withIndex().associate { (i, node) -> node to i }
    val ordered = answered.withIndex()
        .sortedWith(compareBy({ rank[it.value.node] ?: Int.MAX_VALUE }, { it.index }))
        .map { it.value }
    return ClusterOverview(context, ordered, partial = true, expectedNodes = targets.size)
}

/**
 * Collects a streamed overview: [onPartial] gets the nodes so far after each one (see
 * [partialOverview]), the complete overview is returned. Fails if the stream ends without it.
 */
suspend fun Flow<OverviewPart>.collectOverview(
    context: String,
    targets: List<String>,
    onPartial: (ClusterOverview) -> Unit,
): ClusterOverview {
    var answered = emptyList<NodeOverview>()
    var done: ClusterOverview? = null
    collect { part ->
        when (part) {
            is OverviewPart.Node -> {
                answered = answered + part.node
                onPartial(partialOverview(context, answered, targets))
            }
            is OverviewPart.Done -> done = part.overview
        }
    }
    return done ?: error("The overview stream ended without a result")
}

/** How many nodes a partial overview has, of how many (null: not known). */
data class NodesAnswered(val answered: Int, val total: Int?)

/** Null once the overview is complete. A total below the nodes already in is not trusted. */
val ClusterOverview.nodesAnswered: NodesAnswered?
    get() = if (!partial) null else NodesAnswered(nodes.size, expectedNodes.takeIf { it > 0 && it >= nodes.size })
