package name.levis.ichor.ui.overview

import name.levis.ichor.model.KubeTopNodes
import name.levis.ichor.ui.components.NodeUsage
import name.levis.ichor.model.KubeTopNode
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.material3.CardDefaults
import androidx.compose.animation.animateContentSize
import androidx.compose.material.icons.outlined.ExpandLess
import androidx.compose.material.icons.outlined.ExpandMore
import androidx.compose.ui.res.pluralStringResource
import name.levis.ichor.model.status
import name.levis.ichor.ui.components.SectionHeader
import name.levis.ichor.ui.components.expandable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.KeyboardArrowRight
import name.levis.ichor.ui.design.IchorButton
import name.levis.ichor.ui.design.IchorCard
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.KubeNodeInfo
import name.levis.ichor.model.KubeNodesOverview
import name.levis.ichor.model.NodeFilter
import name.levis.ichor.model.address
import name.levis.ichor.model.byStatus
import name.levis.ichor.model.healthy
import name.levis.ichor.model.isDenseCluster
import name.levis.ichor.monitor.CERT_WARN_DAYS
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.importconfig.certExpiry
import name.levis.ichor.ui.kubeauth.SignInAction
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.daysUntil

/** The credentials expire within [CERT_WARN_DAYS] days (or did): a new kubeconfig replaces them. */
@Composable
internal fun KubeCredentialsBanner(cluster: ContextSummary) {
    if (cluster.certNotAfter <= 0 || daysUntil(cluster.certNotAfter) > CERT_WARN_DAYS) return
    val color = if (daysUntil(cluster.certNotAfter) < 0) LocalStatusColors.current.bad else LocalStatusColors.current.warn
    IchorCard(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp)) {
            Text("${stringResource(R.string.import_kube_expires)}: ${certExpiry(cluster.certNotAfter)}", style = MaterialTheme.typography.bodyMedium, color = color)
            MutedText(stringResource(R.string.kube_home_renew))
        }
    }
}

/** The API server did not answer, or refused the credentials: why, and try again. */
@Composable
internal fun KubeUnreachableCard(message: UiText, onRetry: () -> Unit) {
    IchorCard(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(stringResource(R.string.overview_unreachable_title), style = MaterialTheme.typography.titleMedium)
            Text(message.asString(), style = MaterialTheme.typography.bodySmall, color = LocalStatusColors.current.bad)
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                SignInAction(message, onSignedIn = onRetry)
                IchorButton(onClick = onRetry) { Text(stringResource(R.string.common_retry)) }
            }
        }
    }
}

/**
 * The nodes as Kubernetes sees them: roles, readiness, cordon, address, kubelet version,
 * pressure, and where the cloud put them (autoscaler pool, machine type, spot), those needing
 * attention first. A tap opens [onNode]'s actions (cordon, drain): there is no node detail
 * screen, it reads the node through Talos. Like the Talos home's NodesCard, collapsed (the
 * default, [expanded] shared with it) a dense row per node up to FEW_NODES, else a chip per
 * node; expanded, a full row per node with its usage. The header toggles between the two. Past
 * NODE_DENSE_THRESHOLD nodes the card turns dense (see DenseKubeNodes) and the title opens the
 * Kubernetes nodes screen ([onAllNodes]): a card holding hundreds of rows defeats the home.
 * Credentials that may not list nodes get a note instead; the rest of the home still works.
 */
@Composable
internal fun KubeNodesCard(
    overview: KubeNodesOverview,
    /** CPU and memory in use per node, from metrics-server; null without it. */
    top: KubeTopNodes?,
    expanded: Boolean,
    onToggle: () -> Unit,
    onNode: (KubeNodeInfo) -> Unit,
    onAllNodes: (NodeFilter?) -> Unit,
    /** What a full row's swipe and long press lead to. */
    rowActions: KubeNodeRowActions,
) {
    val total = overview.nodes.size
    val dense = isDenseCluster(total)
    val listed = !overview.forbidden && total > 0
    val nodes = remember(overview) { overview.nodes.byStatus().flatMap { it.nodes } }
    IchorCard(Modifier.fillMaxWidth().animateContentSize()) {
        SectionHeader(
            stringResource(R.string.overview_stat_nodes),
            when {
                dense -> Modifier.clickable(role = Role.Button, onClickLabel = stringResource(R.string.overview_nodes_open_all)) { onAllNodes(null) }
                listed -> Modifier.expandable(expanded, onToggle = onToggle)
                else -> Modifier
            }.padding(start = 16.dp, end = 12.dp, top = 12.dp, bottom = 4.dp),
            subtitle = if (overview.forbidden) null else pluralStringResource(R.plurals.overview_summary_nodes, total, total),
            pill = if (listed) nodesPillOf(nodes.map { it.status }) else null,
            chevron = when {
                dense -> Icons.AutoMirrored.Outlined.KeyboardArrowRight
                !listed -> null
                expanded -> Icons.Outlined.ExpandLess
                else -> Icons.Outlined.ExpandMore
            },
        )
        when {
            overview.forbidden -> MutedText(stringResource(R.string.kube_home_nodes_forbidden), modifier = Modifier.padding(start = 16.dp, end = 16.dp, bottom = 16.dp))
            overview.nodes.isEmpty() -> MutedText(stringResource(R.string.kube_home_no_nodes), modifier = Modifier.padding(start = 16.dp, end = 16.dp, bottom = 16.dp))
            dense -> DenseKubeNodes(overview.nodes, onNode, onAllNodes, rowActions)
            !expanded -> KubeNodesCompact(nodes, onNode)
            else -> {
                // No bars without metrics-server.
                val usage = remember(top) { top?.byName.orEmpty() }
                nodes.forEachIndexed { i, node ->
                    if (i > 0) HorizontalDivider(Modifier.padding(horizontal = 16.dp), color = MaterialTheme.colorScheme.outlineVariant)
                    KubeNodeRow(node, onClick = { onNode(node) }, rowActions, modifier = Modifier.padding(horizontal = 16.dp, vertical = 10.dp), usage = usage[node.name])
                }
            }
        }
        Spacer(Modifier.height(4.dp))
    }
}

/**
 * A node's row: name and status, then its details, provenance, CPU and memory in use ([usage],
 * from metrics-server, when there is one) and problem pills; a tap is [onClick], a swipe left or
 * a long press [actions]' (see KubeNodeQuickActions).
 */
@Composable
internal fun KubeNodeRow(node: KubeNodeInfo, onClick: () -> Unit, actions: KubeNodeRowActions, modifier: Modifier = Modifier, usage: KubeTopNode? = null) {
    KubeNodeQuickActions(node, actions, onClick) { KubeNodeRowContent(node, modifier, usage) }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun KubeNodeRowContent(node: KubeNodeInfo, modifier: Modifier, usage: KubeTopNode?) {
    val colors = LocalStatusColors.current
    // Opaque, so the swipe's colour shows only beside the row.
    Column(Modifier.fillMaxWidth().background(CardDefaults.cardColors().containerColor).then(modifier), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(node.name, style = MaterialTheme.typography.titleSmall, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
            if (node.ready) {
                StatusPill(stringResource(R.string.common_status_ready), if (node.healthy) colors.ok else colors.warn)
            } else {
                StatusPill(stringResource(R.string.common_status_not_ready), colors.bad)
            }
        }
        val details = listOf(node.roles.joinToString(", "), node.address, node.kubelet).filter { it.isNotEmpty() }
        if (details.isNotEmpty()) MutedText(details.joinToString("  ·  "), maxLines = 1, overflow = TextOverflow.Ellipsis)
        val provenance = listOfNotNull(nodePoolLabel(node), node.instanceType.ifEmpty { null }, nodeCapacityLabel(node))
        if (provenance.isNotEmpty()) MutedText(provenance.joinToString("  ·  "), maxLines = 1, overflow = TextOverflow.Ellipsis)
        usage?.let { NodeUsage(it) }
        if (node.cordoned || node.pressure.isNotEmpty()) {
            FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                if (node.cordoned) StatusPill(stringResource(R.string.kube_node_cordoned), colors.warn)
                // Kubernetes' own condition names: what kubectl shows too.
                node.pressure.forEach { StatusPill(it, colors.warn) }
            }
        }
    }
}

/** "Karpenter pool general", "GKE node pool default-pool"…; the bare name for a kind this does not know, null for none. */
@Composable
private fun nodePoolLabel(node: KubeNodeInfo): String? = when {
    node.pool.isEmpty() -> null
    node.poolKind == "karpenter" -> stringResource(R.string.kube_node_pool_karpenter, node.pool)
    node.poolKind == "eks" -> stringResource(R.string.kube_node_pool_eks, node.pool)
    node.poolKind == "gke-class" -> stringResource(R.string.kube_node_pool_gke_class, node.pool)
    node.poolKind == "gke" -> stringResource(R.string.kube_node_pool_gke, node.pool)
    node.poolKind == "aks" -> stringResource(R.string.kube_node_pool_aks, node.pool)
    else -> node.pool
}

/** Spot, on-demand or reserved capacity; null when the cloud did not say. */
@Composable
private fun nodeCapacityLabel(node: KubeNodeInfo): String? = when (node.capacity) {
    "spot" -> stringResource(R.string.kube_node_capacity_spot)
    "on-demand" -> stringResource(R.string.kube_node_capacity_on_demand)
    "reserved" -> stringResource(R.string.kube_node_capacity_reserved)
    else -> null
}
