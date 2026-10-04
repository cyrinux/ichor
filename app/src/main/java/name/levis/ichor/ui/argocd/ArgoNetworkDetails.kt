package name.levis.ichor.ui.argocd

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.ListAlt
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.Computer
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.OpenInBrowser
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.ArgoNetNode
import name.levis.ichor.model.NodeOverview
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.dataservices.color
import name.levis.ichor.ui.dataservices.label

/** What the details panel can do with the box it shows; a null action is not offered. */
data class ArgoNetActions(
    /** The Talos node a pod runs on, or the node itself, when the talosconfig targets it. */
    val node: NodeOverview? = null,
    /** Opens [node]'s detail screen on [tab]. */
    val onNode: ((NodeOverview, Int) -> Unit)? = null,
    /** Asks to delete the pod; null while a deletion runs or for anything but a pod. */
    val onDeletePod: (() -> Unit)? = null,
)

/**
 * The tapped box under the graph: kind, namespace/name, health and detail, with what can be
 * done from here: a host opens in the browser, a pod leads to its node's containers (and their
 * logs) or is deleted, a node opens its detail screen.
 */
@Composable
fun ArgoNetDetails(node: ArgoNetNode, podNode: String?, actions: ArgoNetActions, onClose: () -> Unit, modifier: Modifier = Modifier) {
    val accent = node.healthState.color()
    Surface(modifier.fillMaxWidth(), shape = RoundedCornerShape(16.dp), color = MaterialTheme.colorScheme.surfaceContainer) {
        Column(Modifier.padding(start = 16.dp, end = 4.dp, top = 4.dp, bottom = 12.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Icon(node.kindIcon, contentDescription = null, tint = accent, modifier = Modifier.size(20.dp))
                Text(
                    node.name,
                    style = MaterialTheme.typography.titleSmall,
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.padding(start = 10.dp).weight(1f),
                )
                TooltipIconButton(Icons.Outlined.Close, stringResource(R.string.argo_net_close), onClick = onClose)
            }
            Column(Modifier.padding(end = 12.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                Text(
                    listOf(node.kindLabel(), node.namespace, node.healthState.label()).filter { it.isNotEmpty() }.joinToString(" · "),
                    style = MaterialTheme.typography.labelMedium,
                    color = accent,
                )
                if (node.detail.isNotEmpty()) {
                    Text(node.detail, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace)
                }
                ownership(node)?.let { MutedText(stringResource(it)) }
                podNode?.let { MutedText(stringResource(R.string.argo_net_runs_on, it)) }
                DetailActions(node, actions)
            }
        }
    }
}

private fun ownership(node: ArgoNetNode): Int? = when {
    node.managed -> R.string.argo_net_managed
    node.shared -> R.string.argo_net_shared
    else -> null
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun DetailActions(node: ArgoNetNode, actions: ArgoNetActions) {
    val uri = LocalUriHandler.current
    val target = actions.node
    val onNode = actions.onNode
    FlowRow(Modifier.padding(top = 8.dp), horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        if (node.kind == ArgoNetNode.HOST && node.url.isNotEmpty()) {
            Action(Icons.Outlined.OpenInBrowser, stringResource(R.string.argo_net_open)) { runCatching { uri.openUri(node.url) } }
        }
        if (node.kind == ArgoNetNode.POD && target != null && onNode != null) {
            Action(Icons.AutoMirrored.Outlined.ListAlt, stringResource(R.string.argo_net_pod_containers, target.hostname)) { onNode(target, NODE_PODS_TAB) }
        }
        if (node.kind == ArgoNetNode.NODE && target != null && onNode != null) {
            Action(Icons.Outlined.Computer, stringResource(R.string.argo_net_open_node)) { onNode(target, 0) }
        }
        if (node.kind == ArgoNetNode.POD) {
            OutlinedButton(onClick = { actions.onDeletePod?.invoke() }, enabled = actions.onDeletePod != null) {
                Icon(Icons.Outlined.Delete, contentDescription = null, modifier = Modifier.size(18.dp))
                Text(stringResource(R.string.common_delete), modifier = Modifier.padding(start = 6.dp))
            }
        }
    }
}

@Composable
private fun Action(icon: ImageVector, label: String, onClick: () -> Unit) {
    FilledTonalButton(onClick = onClick) {
        Icon(icon, contentDescription = null, modifier = Modifier.size(18.dp))
        Text(label, modifier = Modifier.padding(start = 6.dp), maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}

/** The node detail screen's Pods tab: containers, and their logs. */
private const val NODE_PODS_TAB = 4
