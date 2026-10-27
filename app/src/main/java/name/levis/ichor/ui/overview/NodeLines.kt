package name.levis.ichor.ui.overview

import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.padding
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.CustomAccessibilityAction
import androidx.compose.ui.semantics.customActions
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.NodeGroup
import name.levis.ichor.model.NodeOverview
import name.levis.ichor.model.NodeStatus
import name.levis.ichor.model.ServiceHealth
import name.levis.ichor.model.status
import name.levis.ichor.ui.components.ResourceRow
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.components.Tag
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.tour.TourAnchor
import name.levis.ichor.ui.tour.tourAnchor

/** Up to this many nodes, the collapsed Nodes card lists them as rows; past it, as chips. */
internal const val FEW_NODES = 4

/**
 * The Nodes card's one pill: "1 unreachable" in red, else "2 not ready" in amber; none while
 * every node is ready. The summary card and this one are the only places that say it.
 */
internal fun nodesPill(nodes: List<NodeOverview>): (@Composable () -> Unit)? = nodesPillOf(nodes.map { it.status })

/** The same pill from the nodes' statuses: a Kubernetes cluster has no unreachable ones. */
internal fun nodesPillOf(statuses: List<NodeStatus>): (@Composable () -> Unit)? {
    val unreachable = statuses.count { it == NodeStatus.UNREACHABLE }
    val notReady = statuses.count { it == NodeStatus.NOT_READY }
    if (unreachable == 0 && notReady == 0) return null
    return { NodesPill(unreachable, notReady) }
}

@Composable
private fun NodesPill(unreachable: Int, notReady: Int) {
    val colors = LocalStatusColors.current
    if (unreachable > 0) {
        StatusPill("$unreachable ${pluralStringResource(R.plurals.overview_summary_unreachable, unreachable)}", colors.bad)
    } else {
        StatusPill("$notReady ${pluralStringResource(R.plurals.overview_summary_not_ready, notReady)}", colors.warn)
    }
}

/** A dense row per node, for a cluster small enough to list: tap opens it, a long press its actions. */
@Composable
internal fun NodeLines(
    nodes: List<NodeOverview>,
    onNode: (NodeOverview) -> Unit,
    onLive: (NodeOverview) -> Unit,
    onMore: (NodeOverview) -> Unit,
    /** The node the demo tour points at, if listed here. */
    degraded: NodeOverview? = null,
) {
    val liveLabel = stringResource(R.string.overview_action_live_graphs)
    val actionsLabel = stringResource(R.string.overview_node_actions)
    Column(Modifier.padding(horizontal = 16.dp)) {
        nodes.forEach { node ->
            val status = node.status
            ResourceRow(
                status.serviceHealth,
                node.hostname,
                Modifier.degradedAnchor(node, degraded).combinedClickable(
                    onClick = { onNode(node) },
                    onLongClick = { onMore(node) },
                    onLongClickLabel = actionsLabel,
                ).semantics {
                    customActions = listOf(CustomAccessibilityAction(liveLabel) { onLive(node); true })
                },
                subtitle = statusLabel(status).takeIf { status != NodeStatus.READY },
                trailing = {
                    if (node.cordoned) Tag(stringResource(R.string.kube_node_cordoned), LocalStatusColors.current.warn)
                },
            )
        }
    }
}

/** The first node not ready, in the order the card lists them (unreachable first in each group). */
internal fun List<NodeGroup>.firstDegraded(): NodeOverview? =
    asSequence().flatMap { g -> g.nodes.sortedBy { it.status != NodeStatus.UNREACHABLE } }.firstOrNull { it.status != NodeStatus.READY }

/** Marks [node] as the demo tour's degraded node when it is [degraded]. */
internal fun Modifier.degradedAnchor(node: NodeOverview, degraded: NodeOverview?): Modifier =
    if (node == degraded) tourAnchor(TourAnchor.DegradedNode) else this

internal val NodeStatus.serviceHealth: ServiceHealth
    get() = when (this) {
        NodeStatus.READY -> ServiceHealth.OK
        NodeStatus.ATTENTION, NodeStatus.NOT_READY -> ServiceHealth.WARNING
        NodeStatus.UNREACHABLE -> ServiceHealth.CRITICAL
    }
