package name.levis.ichor.ui.overview

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.KeyboardArrowRight
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.key
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.CustomAccessibilityAction
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.customActions
import androidx.compose.ui.semantics.isTraversalGroup
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.HealthCounts
import name.levis.ichor.model.NodeFilter
import name.levis.ichor.model.NodeGroup
import name.levis.ichor.model.NodeOverview
import name.levis.ichor.model.NodeStatus
import name.levis.ichor.model.healthCounts
import name.levis.ichor.model.problemNodes
import name.levis.ichor.model.status
import name.levis.ichor.ui.kubespan.siteTitle
import name.levis.ichor.ui.theme.LocalStatusColors

/** Touch target of a node's dot: small enough for a few hundred, big enough to hit. */
private val DOT_TARGET = 24.dp
private val DOT_SIZE = 12.dp

/**
 * The dense Nodes card's body, for clusters too large for a chip per node: the health counts,
 * a dot per node (site by site when there are several), then the problem nodes as full rows,
 * capped, with "N more" opening the Nodes screen on them ([onAllNodes]).
 */
@Composable
internal fun DenseNodes(
    groups: List<NodeGroup>,
    publicIps: PublicIpDetection,
    sharedVersion: String?,
    onNode: (NodeOverview) -> Unit,
    onLive: (NodeOverview) -> Unit,
    onMore: (NodeOverview) -> Unit,
    onAllNodes: (NodeFilter?) -> Unit,
) {
    val problems = remember(groups) { groups.flatMap { it.nodes }.problemNodes() }
    groups.forEachIndexed { g, group ->
        val counts = remember(group) { group.nodes.healthCounts() }
        // One line per site with its counts; a single site says nothing, only the counts.
        val summary = healthSummary(counts)
        val line = if (groups.size > 1) siteLine(siteLabel(group), summary) else summary
        Text(
            line,
            style = MaterialTheme.typography.labelLarge,
            maxLines = 2,
            overflow = TextOverflow.Ellipsis,
            modifier = Modifier.padding(start = 16.dp, end = 16.dp, top = if (g > 0) 12.dp else 4.dp, bottom = 2.dp),
        )
        NodeDots(group.nodes, onNode, onLive, onMore)
    }
    problems.shown.forEach { node ->
        HorizontalDivider(Modifier.padding(horizontal = 16.dp), color = MaterialTheme.colorScheme.outlineVariant)
        SwipeableNodeRow(node, publicIps, sharedVersion, onNode, onLive, onMore)
    }
    if (problems.more > 0) {
        HorizontalDivider(Modifier.padding(horizontal = 16.dp), color = MaterialTheme.colorScheme.outlineVariant)
        MoreProblems(problems.more) { onAllNodes(NodeFilter.ATTENTION) }
    }
}

/** A site's name, or the nodes the map does not place. */
@Composable
internal fun siteLabel(group: NodeGroup): String =
    group.site?.let { siteTitle(it) } ?: stringResource(R.string.overview_nodes_unplaced)

/**
 * "187 ready · 2 need a look · 3 not ready · 1 unreachable", each count in its status colour;
 * zeros left out.
 */
@Composable
internal fun healthSummary(counts: HealthCounts): AnnotatedString {
    val parts = NodeStatus.entries.filter { counts[it] > 0 }.map { status ->
        val n = counts[status]
        pluralStringResource(countLabel(status), n, n) to statusColor(status)
    }
    return buildAnnotatedString {
        parts.forEachIndexed { i, (text, color) ->
            if (i > 0) append("  ·  ")
            withStyle(SpanStyle(color = color)) { append(text) }
        }
    }
}

@Composable
private fun siteLine(site: String, summary: AnnotatedString): AnnotatedString {
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    return buildAnnotatedString {
        withStyle(SpanStyle(color = muted)) { append(site) }
        append("  ·  ")
        append(summary)
    }
}

private fun countLabel(status: NodeStatus) = when (status) {
    NodeStatus.READY -> R.plurals.overview_nodes_count_ready
    NodeStatus.ATTENTION -> R.plurals.overview_nodes_count_attention
    NodeStatus.NOT_READY -> R.plurals.overview_nodes_count_not_ready
    NodeStatus.UNREACHABLE -> R.plurals.overview_nodes_count_unreachable
}

/** Warn for a ready node reporting a problem too: it is listed as one, it must not look calm. */
@Composable
private fun statusColor(status: NodeStatus): Color {
    val colors = LocalStatusColors.current
    return when (status) {
        NodeStatus.READY -> colors.ok
        NodeStatus.ATTENTION, NodeStatus.NOT_READY -> colors.warn
        NodeStatus.UNREACHABLE -> colors.bad
    }
}

@Composable
private fun statusLabel(status: NodeStatus): String = stringResource(
    when (status) {
        NodeStatus.READY -> R.string.common_status_ready
        NodeStatus.ATTENTION -> R.string.nodes_filter_attention
        NodeStatus.NOT_READY -> R.string.common_status_not_ready
        NodeStatus.UNREACHABLE -> R.string.common_status_unreachable
    },
)

/** A dot per node, coloured by its status; one traversal group, so TalkBack reads them in order. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun NodeDots(
    nodes: List<NodeOverview>,
    onNode: (NodeOverview) -> Unit,
    onLive: (NodeOverview) -> Unit,
    onMore: (NodeOverview) -> Unit,
) {
    FlowRow(Modifier.fillMaxWidth().padding(horizontal = 10.dp, vertical = 4.dp).semantics { isTraversalGroup = true }) {
        nodes.forEach { node ->
            key(node.node) {
                // An unreachable node has nothing to open: a tap offers its actions (Wake-on-LAN…).
                NodeDot(
                    node,
                    onClick = { if (node.reachable) onNode(node) else onMore(node) },
                    onLongClick = { onMore(node) },
                    onLive = { onLive(node) }.takeIf { node.reachable },
                )
            }
        }
    }
}

/** Tap opens the node, a long press its actions; read as "hostname, status". */
@Composable
private fun NodeDot(node: NodeOverview, onClick: () -> Unit, onLongClick: () -> Unit, onLive: (() -> Unit)?) {
    val description = "${node.hostname}, ${statusLabel(node.status)}"
    val liveLabel = stringResource(R.string.overview_action_live_graphs)
    val color = statusColor(node.status)
    Box(
        Modifier.size(DOT_TARGET).combinedClickable(
            role = Role.Button,
            onClick = onClick,
            onLongClick = onLongClick,
            onLongClickLabel = stringResource(R.string.overview_node_actions),
        ).semantics {
            contentDescription = description
            customActions = listOfNotNull(onLive?.let { CustomAccessibilityAction(liveLabel) { it(); true } })
        },
        contentAlignment = Alignment.Center,
    ) {
        Box(Modifier.size(DOT_SIZE).background(color, CircleShape))
    }
}

/** The problem nodes past the cap: opens the Nodes screen on them. */
@Composable
private fun MoreProblems(count: Int, onClick: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().clickable(role = Role.Button, onClick = onClick).padding(start = 16.dp, end = 12.dp, top = 12.dp, bottom = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            pluralStringResource(R.plurals.overview_nodes_more_problems, count, count),
            style = MaterialTheme.typography.labelLarge,
            color = MaterialTheme.colorScheme.primary,
            modifier = Modifier.weight(1f),
        )
        Icon(Icons.AutoMirrored.Outlined.KeyboardArrowRight, contentDescription = null, tint = MaterialTheme.colorScheme.primary)
    }
}
