package name.levis.ichor.ui.overview

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.KubeNodeInfo
import name.levis.ichor.model.KubeNodesOverview
import name.levis.ichor.model.address
import name.levis.ichor.model.healthy
import name.levis.ichor.model.readyCount
import name.levis.ichor.monitor.CERT_WARN_DAYS
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.InfoRow
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.importconfig.certExpiry
import name.levis.ichor.ui.kubeauth.SignInAction
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.daysUntil

/** Nodes listed on the home card; a large cluster's others are only counted. */
private const val MAX_HOME_NODES = 30

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
            if (cluster.namespace.isNotEmpty()) InfoRow(stringResource(R.string.import_kube_namespace), cluster.namespace)
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
 * The nodes as Kubernetes sees them: roles, readiness, cordon, address, kubelet version and
 * pressure. Not tappable: a node's detail screens read it through Talos. Credentials that may
 * not list nodes get a note instead; the rest of the home still works.
 */
@Composable
internal fun KubeNodesCard(overview: KubeNodesOverview) {
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            Text(stringResource(R.string.common_label_nodes), style = MaterialTheme.typography.titleMedium)
            when {
                overview.forbidden -> MutedText(stringResource(R.string.kube_home_nodes_forbidden))
                overview.nodes.isEmpty() -> MutedText(stringResource(R.string.kube_home_no_nodes))
                else -> {
                    overview.nodes.take(MAX_HOME_NODES).forEachIndexed { i, node ->
                        if (i > 0) HorizontalDivider()
                        KubeNodeRow(node)
                    }
                    val more = overview.nodes.size - MAX_HOME_NODES
                    if (more > 0) MutedText(stringResource(R.string.kube_home_more_nodes, more))
                }
            }
        }
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun KubeNodeRow(node: KubeNodeInfo) {
    val colors = LocalStatusColors.current
    Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
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
        if (node.cordoned || node.pressure.isNotEmpty()) {
            FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                if (node.cordoned) StatusPill(stringResource(R.string.kube_node_cordoned), colors.warn)
                // Kubernetes' own condition names: what kubectl shows too.
                node.pressure.forEach { StatusPill(it, colors.warn) }
            }
        }
    }
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
                OutlinedButton(onClick = nav.onMetrics) { Text(stringResource(R.string.metrics_title)) }
                OutlinedButton(onClick = nav.onCheckup) { Text(stringResource(R.string.checkup_title)) }
                OutlinedButton(onClick = nav.onApiHealth) { Text(stringResource(R.string.apihealth_title)) }
                OutlinedButton(onClick = nav.onNetworkPolicies) { Text(stringResource(R.string.netpol_title)) }
            }
        }
    }
}
