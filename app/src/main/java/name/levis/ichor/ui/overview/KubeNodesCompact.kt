package name.levis.ichor.ui.overview

import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.KubeNodeInfo
import name.levis.ichor.model.NodeStatus
import name.levis.ichor.model.status
import name.levis.ichor.ui.components.ResourceRow
import name.levis.ichor.ui.components.Tag
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * The collapsed Kubernetes Nodes card, as the Talos one: a dense row per node up to FEW_NODES,
 * else a chip per node. No node screen without Talos: a tap and a long press both open the
 * node's actions ([onNode]).
 */
@Composable
internal fun KubeNodesCompact(nodes: List<KubeNodeInfo>, onNode: (KubeNodeInfo) -> Unit) {
    if (nodes.size <= FEW_NODES) KubeNodeLines(nodes, onNode) else KubeNodeChips(nodes, onNode)
}

@Composable
private fun KubeNodeLines(nodes: List<KubeNodeInfo>, onNode: (KubeNodeInfo) -> Unit) {
    val actionsLabel = stringResource(R.string.overview_node_actions)
    Column(Modifier.padding(horizontal = 16.dp)) {
        nodes.forEach { node ->
            val status = node.status
            ResourceRow(
                status.serviceHealth,
                node.name,
                Modifier.combinedClickable(onClick = { onNode(node) }, onLongClick = { onNode(node) }, onLongClickLabel = actionsLabel),
                subtitle = statusLabel(status).takeIf { status != NodeStatus.READY },
                trailing = {
                    if (node.cordoned) Tag(stringResource(R.string.kube_node_cordoned), LocalStatusColors.current.warn)
                },
            )
        }
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun KubeNodeChips(nodes: List<KubeNodeInfo>, onNode: (KubeNodeInfo) -> Unit) {
    FlowRow(
        horizontalArrangement = Arrangement.spacedBy(8.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
        modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp),
    ) {
        nodes.forEach { node ->
            NodeStatusChip(node.name, node.status, node.cordoned, onClick = { onNode(node) }, onLongClick = { onNode(node) })
        }
    }
}
