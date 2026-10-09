package name.levis.ichor.ui.overview

import androidx.compose.foundation.clickable
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
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
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
import name.levis.ichor.model.readyCount
import name.levis.ichor.monitor.CERT_WARN_DAYS
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.InfoRow
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.rememberKubeWhoAmI
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
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp)) {
            Text("${stringResource(R.string.import_kube_expires)}: ${certExpiry(cluster.certNotAfter)}", style = MaterialTheme.typography.bodyMedium, color = color)
            MutedText(stringResource(R.string.kube_home_renew))
        }
    }
}

/** The API server did not answer, or refused the credentials: why, and try again. */
@Composable
internal fun KubeUnreachableCard(message: UiText, onRetry: () -> Unit) {
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(stringResource(R.string.overview_unreachable_title), style = MaterialTheme.typography.titleMedium)
            Text(message.asString(), style = MaterialTheme.typography.bodySmall, color = LocalStatusColors.current.bad)
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                SignInAction(message, onSignedIn = onRetry)
                Button(onClick = onRetry) { Text(stringResource(R.string.common_retry)) }
            }
        }
    }
}

/**
 * The cluster at a glance: its API server and version, how many nodes are ready, and who the
 * credentials are. [nodes] is null until the API server answered.
 */
@Composable
internal fun KubeSummaryCard(name: String, cluster: ContextSummary, nodes: KubeNodesOverview?) {
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) {
                    Text(name, style = MaterialTheme.typography.titleMedium, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    val version = nodes?.serverVersion?.takeIf { it.isNotEmpty() }
                    MutedText(version?.let { stringResource(R.string.kube_home_version, it) } ?: stringResource(R.string.common_kind_kubernetes))
                }
                nodes?.takeIf { !it.forbidden && it.nodes.isNotEmpty() }?.let { KubeStatusPill(it) }
            }
            HorizontalDivider(Modifier.padding(vertical = 12.dp))
            InfoRow(stringResource(R.string.import_kube_server), cluster.endpoints.joinToString("\n"), mono = true)
            if (nodes != null && !nodes.forbidden) {
                InfoRow(stringResource(R.string.overview_stat_nodes), "${nodes.readyCount}/${nodes.nodes.size}")
            }
            if (cluster.user.isNotEmpty()) InfoRow(stringResource(R.string.import_kube_user), cluster.user)
            // Who the API server takes the credentials for (an OIDC or cloud identity); hidden when it cannot say.
            rememberKubeWhoAmI(cluster.name)?.let { who ->
                InfoRow(stringResource(R.string.kube_whoami_user), listOf(who.user, who.groups.joinToString(", ")).filter { it.isNotEmpty() }.joinToString("\n"), mono = true)
            }
            if (cluster.certNotAfter > 0) InfoRow(stringResource(R.string.import_kube_expires), certExpiry(cluster.certNotAfter))
        }
    }
}

@Composable
private fun KubeStatusPill(nodes: KubeNodesOverview) {
    val colors = LocalStatusColors.current
    when (nodes.readyCount) {
        nodes.nodes.size -> StatusPill(stringResource(R.string.overview_status_healthy), colors.ok)
        0 -> StatusPill(stringResource(R.string.overview_status_down), colors.bad)
        else -> StatusPill(stringResource(R.string.overview_status_degraded), colors.warn)
    }
}

/**
 * The nodes as Kubernetes sees them: roles, readiness, cordon, address, kubelet version,
 * pressure, and where the cloud put them (autoscaler pool, machine type, spot), those needing
 * attention first. A tap opens [onNode]'s actions (cordon, drain): there is no node detail
 * screen, it reads the node through Talos. Past NODE_DENSE_THRESHOLD nodes the card turns
 * dense (see DenseKubeNodes) and the title opens the Kubernetes nodes screen ([onAllNodes]):
 * a card holding hundreds of rows defeats the home. Credentials that may not list nodes get a
 * note instead; the rest of the home still works.
 */
@Composable
internal fun KubeNodesCard(overview: KubeNodesOverview, onNode: (KubeNodeInfo) -> Unit, onAllNodes: (NodeFilter?) -> Unit) {
    val dense = isDenseCluster(overview.nodes.size)
    Card(Modifier.fillMaxWidth()) {
        Row(
            (if (dense) Modifier.clickable(role = Role.Button, onClickLabel = stringResource(R.string.overview_nodes_open_all)) { onAllNodes(null) } else Modifier)
                .padding(start = 16.dp, end = 12.dp, top = 12.dp, bottom = 4.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(stringResource(R.string.common_label_nodes), style = MaterialTheme.typography.titleMedium, modifier = Modifier.weight(1f))
            if (!overview.forbidden) {
                Text(overview.nodes.size.toString(), style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            if (dense) {
                Icon(
                    Icons.AutoMirrored.Outlined.KeyboardArrowRight,
                    contentDescription = null,
                    tint = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.padding(start = 4.dp),
                )
            }
        }
        when {
            overview.forbidden -> MutedText(stringResource(R.string.kube_home_nodes_forbidden), modifier = Modifier.padding(start = 16.dp, end = 16.dp, bottom = 16.dp))
            overview.nodes.isEmpty() -> MutedText(stringResource(R.string.kube_home_no_nodes), modifier = Modifier.padding(start = 16.dp, end = 16.dp, bottom = 16.dp))
            dense -> DenseKubeNodes(overview.nodes, onNode, onAllNodes)
            else -> {
                val nodes = remember(overview) { overview.nodes.byStatus().flatMap { it.nodes } }
                nodes.forEachIndexed { i, node ->
                    if (i > 0) HorizontalDivider(Modifier.padding(horizontal = 16.dp), color = MaterialTheme.colorScheme.outlineVariant)
                    KubeNodeRow(node, onClick = { onNode(node) }, modifier = Modifier.padding(horizontal = 16.dp, vertical = 10.dp))
                }
            }
        }
        Spacer(Modifier.height(4.dp))
    }
}

/** A node's row: name and status, then its details, provenance and problem pills; a tap is [onClick]. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun KubeNodeRow(node: KubeNodeInfo, onClick: () -> Unit, modifier: Modifier = Modifier) {
    val colors = LocalStatusColors.current
    Column(Modifier.fillMaxWidth().clickable(onClick = onClick).then(modifier), verticalArrangement = Arrangement.spacedBy(4.dp)) {
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

/** The Kubernetes screens that work with these credentials alone. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun KubeToolsCard(nav: KubeHomeNavigation) {
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(stringResource(R.string.common_kind_kubernetes), style = MaterialTheme.typography.titleMedium)
            FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedButton(onClick = nav.onWorkloads) { Text(stringResource(R.string.overview_action_workloads)) }
                OutlinedButton(onClick = nav.onResources) { Text(stringResource(R.string.kb_title)) }
                OutlinedButton(onClick = nav.onHelm) { Text(stringResource(R.string.kb_helm_title)) }
                OutlinedButton(onClick = nav.onMetrics) { Text(stringResource(R.string.metrics_title)) }
                OutlinedButton(onClick = { nav.onDataServices(null) }) { Text(stringResource(R.string.data_services_title)) }
                OutlinedButton(onClick = nav.onCheckup) { Text(stringResource(R.string.checkup_title)) }
                OutlinedButton(onClick = nav.onApiHealth) { Text(stringResource(R.string.apihealth_title)) }
                OutlinedButton(onClick = nav.onNetworkPolicies) { Text(stringResource(R.string.netpol_title)) }
            }
        }
    }
}
