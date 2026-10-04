package name.levis.ichor.ui.overview

import androidx.compose.animation.animateContentSize
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.background
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.ExpandLess
import androidx.compose.material.icons.outlined.ExpandMore
import androidx.compose.material.icons.outlined.Lan
import androidx.compose.material.icons.outlined.Public
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.CustomAccessibilityAction
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.customActions
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import name.levis.ichor.R
import name.levis.ichor.model.NodeGroup
import name.levis.ichor.model.NodeOverview
import name.levis.ichor.model.health
import name.levis.ichor.model.needsAttention
import name.levis.ichor.model.sharedVersion
import name.levis.ichor.model.shownPublicIps
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.NodeHealthPill
import name.levis.ichor.ui.components.agoLabel
import name.levis.ichor.ui.components.expandable
import name.levis.ichor.ui.kubespan.siteTitle
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * The nodes as one card, like Apps and Data services. Collapsed (the default), a chip per calm
 * node and a full row only for those needing attention, so a problem never hides behind the
 * fold; expanded, a swipeable row per node. The title toggles between the two.
 */
@Composable
fun NodesCard(
    groups: List<NodeGroup>,
    publicIps: PublicIpDetection,
    expanded: Boolean,
    onToggle: () -> Unit,
    onNode: (NodeOverview) -> Unit,
    onLive: (NodeOverview) -> Unit,
    onMore: (NodeOverview) -> Unit,
    titleModifier: Modifier = Modifier,
) {
    // The cluster summary shows the version all nodes share; the rows only say it when it differs.
    val sharedVersion = remember(groups) { groups.flatMap { it.nodes }.sharedVersion() }
    Card(Modifier.fillMaxWidth().animateContentSize()) {
        Row(
            titleModifier.expandable(expanded, onToggle = onToggle)
                .padding(start = 16.dp, end = 12.dp, top = 12.dp, bottom = 4.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(stringResource(R.string.overview_stat_nodes), style = MaterialTheme.typography.titleMedium, modifier = Modifier.weight(1f))
            DetectPublicIpsButton(publicIps)
            Spacer(Modifier.width(8.dp))
            Text(groups.sumOf { it.nodes.size }.toString(), style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
            Icon(
                if (expanded) Icons.Outlined.ExpandLess else Icons.Outlined.ExpandMore,
                contentDescription = null,
                tint = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(start = 4.dp),
            )
        }
        groups.forEachIndexed { g, group ->
            // Site headers only when there is more than one: a single site says nothing.
            if (groups.size > 1) Text(
                group.site?.let { siteTitle(it) } ?: stringResource(R.string.overview_nodes_unplaced),
                style = MaterialTheme.typography.labelLarge,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.padding(start = 16.dp, end = 16.dp, top = if (g > 0) 12.dp else 4.dp, bottom = 2.dp),
            )
            val (calm, rows) = if (expanded) emptyList<NodeOverview>() to group.nodes else group.nodes.partition { !it.needsAttention }
            if (calm.isNotEmpty()) NodeChips(calm, onNode, onLive, onMore)
            rows.forEachIndexed { i, node ->
                if (i > 0 || calm.isNotEmpty()) HorizontalDivider(Modifier.padding(horizontal = 16.dp), color = MaterialTheme.colorScheme.outlineVariant)
                SwipeableNode(node, onLive = { onLive(node) }, onMore = { onMore(node) }) {
                    NodeRow(
                        node,
                        node.shownPublicIps(publicIps.probed),
                        sharedVersion,
                        onClick = { onNode(node) },
                        onLongClick = { onMore(node) },
                        onLive = { onLive(node) }.takeIf { node.reachable },
                    )
                }
            }
        }
        Spacer(Modifier.height(4.dp))
    }
}

/** Calm nodes, a chip each: tap opens the node, a long press its actions. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun NodeChips(
    nodes: List<NodeOverview>,
    onNode: (NodeOverview) -> Unit,
    onLive: (NodeOverview) -> Unit,
    onMore: (NodeOverview) -> Unit,
) {
    FlowRow(
        horizontalArrangement = Arrangement.spacedBy(8.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
        modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp),
    ) {
        nodes.forEach { node -> NodeChip(node, onClick = { onNode(node) }, onLongClick = { onMore(node) }, onLive = { onLive(node) }) }
    }
}

@Composable
private fun NodeChip(node: NodeOverview, onClick: () -> Unit, onLongClick: () -> Unit, onLive: () -> Unit) {
    val liveLabel = stringResource(R.string.overview_action_live_graphs)
    val status = stringResource(R.string.common_status_ready)
    val color = LocalStatusColors.current.ok
    Surface(
        shape = RoundedCornerShape(50),
        color = MaterialTheme.colorScheme.surfaceVariant,
        modifier = Modifier.combinedClickable(
            onClick = onClick,
            onLongClick = onLongClick,
            onLongClickLabel = stringResource(R.string.overview_node_actions),
        ).semantics(mergeDescendants = true) {
            contentDescription = "${node.hostname}, $status"
            customActions = listOf(CustomAccessibilityAction(liveLabel) { onLive(); true })
        },
    ) {
        Row(Modifier.padding(horizontal = 12.dp, vertical = 6.dp), verticalAlignment = Alignment.CenterVertically) {
            Box(Modifier.size(8.dp).background(color, CircleShape))
            Spacer(Modifier.width(6.dp))
            Text(node.hostname, style = MaterialTheme.typography.labelLarge, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
    }
}

@Composable
private fun NodeRow(
    node: NodeOverview,
    publicIps: List<String>,
    sharedVersion: String?,
    onClick: () -> Unit,
    onLongClick: () -> Unit,
    onLive: (() -> Unit)?,
) {
    val liveLabel = stringResource(R.string.overview_action_live_graphs)
    val version = node.version.takeIf { it != sharedVersion }.orEmpty()
    // Opaque, so the swipe background only shows beside the row as it slides.
    Box(
        Modifier.fillMaxWidth().background(CardDefaults.cardColors().containerColor).combinedClickable(
            onClick = { if (node.reachable) onClick() },
            onLongClick = onLongClick,
            onLongClickLabel = stringResource(R.string.overview_node_actions),
        ).semantics {
            // Swiping right, without the gesture; swiping left is the long-press above.
            customActions = listOfNotNull(onLive?.let { CustomAccessibilityAction(liveLabel) { it(); true } })
        },
    ) {
        Column(Modifier.padding(horizontal = 16.dp, vertical = 10.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(node.hostname, style = MaterialTheme.typography.titleMedium, modifier = Modifier.weight(1f))
                NodeHealthPill(node.health)
            }
            // Below the pill rather than beside it: the full width keeps an IPv6 on one line.
            Column(Modifier.padding(top = 2.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                AddressLine(Icons.Outlined.Lan, node.node)
                if (node.reachable && publicIps.isNotEmpty()) PublicAddresses(publicIps)
            }
            if (node.reachable) {
                Text(
                    listOf(roleLabel(node.role), version, node.stage, node.arch)
                        .filter { it.isNotBlank() }
                        .joinToString("  ·  "),
                    style = MaterialTheme.typography.bodySmall,
                    modifier = Modifier.padding(top = 4.dp),
                )
            }
            // No longer answering: what it was when it last did, dimmed.
            node.lastSeen?.let { seen ->
                MutedText(
                    listOf(roleLabel(node.role), node.version, node.arch).filter { it.isNotBlank() }.joinToString("  ·  "),
                    modifier = Modifier.padding(top = 4.dp),
                )
                MutedText(stringResource(R.string.overview_node_last_seen, agoLabel(System.currentTimeMillis() - seen)))
            }
            node.unmetConditions.forEach {
                Text(
                    "${it.name}: ${it.reason}",
                    style = MaterialTheme.typography.bodySmall,
                    color = LocalStatusColors.current.warn,
                    modifier = Modifier.padding(top = 4.dp),
                )
            }
            node.error?.let {
                Text(
                    it,
                    style = MaterialTheme.typography.bodySmall,
                    color = LocalStatusColors.current.bad,
                    modifier = Modifier.padding(top = 4.dp),
                )
            }
        }
    }
}

/** The node's internet-facing addresses, one per line under its talosconfig one. */
@Composable
private fun PublicAddresses(addresses: List<String>) {
    val label = stringResource(R.string.overview_node_public_ip)
    Column(
        verticalArrangement = Arrangement.spacedBy(2.dp),
        modifier = Modifier.semantics(mergeDescendants = true) { contentDescription = "$label: ${addresses.joinToString()}" },
    ) {
        addresses.forEach { AddressLine(Icons.Outlined.Public, it) }
    }
}

/** An address behind a fixed-size icon, so private and public ones line up. */
@Composable
private fun AddressLine(icon: ImageVector, address: String) {
    Row(verticalAlignment = Alignment.CenterVertically) {
        Icon(
            icon,
            contentDescription = null,
            tint = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.size(14.dp),
        )
        Spacer(Modifier.width(6.dp))
        Text(
            address,
            style = MaterialTheme.typography.labelSmall,
            fontFamily = FontFamily.Monospace,
            letterSpacing = 0.sp,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}

@Composable
private fun roleLabel(role: String) = when (role) {
    "controlplane" -> stringResource(R.string.overview_role_control_plane)
    else -> role
}
