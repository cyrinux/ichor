package name.levis.ichor.ui.overview

import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.key
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.isTraversalGroup
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.model.KubeNodeInfo
import name.levis.ichor.model.NodeFilter
import name.levis.ichor.model.byStatus
import name.levis.ichor.model.kubeHealthCounts
import name.levis.ichor.model.kubeProblemNodes
import name.levis.ichor.model.status

/**
 * The Kubernetes home's Nodes card on a cluster too large for a row per node (see
 * isDenseCluster), as the Talos home's DenseNodes: the status counts, a dot per node grouped
 * by status (the worst first), then the problem nodes as full rows, capped, with "N more"
 * opening the Kubernetes nodes screen on them ([onAllNodes]). A tap on a dot or a row opens
 * the node's screen ([onNode]).
 */
@Composable
internal fun DenseKubeNodes(nodes: List<KubeNodeInfo>, onNode: (KubeNodeInfo) -> Unit, onAllNodes: (NodeFilter?) -> Unit) {
    val counts = remember(nodes) { nodes.kubeHealthCounts() }
    val groups = remember(nodes) { nodes.byStatus() }
    val problems = remember(nodes) { nodes.kubeProblemNodes() }
    Text(
        healthSummary(counts),
        style = MaterialTheme.typography.labelLarge,
        maxLines = 2,
        overflow = TextOverflow.Ellipsis,
        modifier = Modifier.padding(start = 16.dp, end = 16.dp, top = 4.dp, bottom = 2.dp),
    )
    KubeNodeDots(groups.flatMap { it.nodes }, onNode)
    problems.shown.forEach { node ->
        HorizontalDivider(Modifier.padding(horizontal = 16.dp), color = MaterialTheme.colorScheme.outlineVariant)
        KubeNodeRow(node, onClick = { onNode(node) }, modifier = Modifier.padding(horizontal = 16.dp, vertical = 10.dp))
    }
    if (problems.more > 0) {
        HorizontalDivider(Modifier.padding(horizontal = 16.dp), color = MaterialTheme.colorScheme.outlineVariant)
        MoreProblems(problems.more) { onAllNodes(NodeFilter.ATTENTION) }
    }
}

/**
 * A dot per node, coloured by its status, in the given order (grouped by status: the problem
 * dots lead); one traversal group, so TalkBack reads them in order.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun KubeNodeDots(nodes: List<KubeNodeInfo>, onNode: (KubeNodeInfo) -> Unit) {
    FlowRow(Modifier.fillMaxWidth().padding(horizontal = 10.dp, vertical = 4.dp).semantics { isTraversalGroup = true }) {
        nodes.forEach { node ->
            key(node.name) {
                // No node screen to open: the tap and the long press both offer the actions.
                StatusDot(
                    description = "${node.name}, ${statusLabel(node.status)}",
                    color = statusColor(node.status),
                    onClick = { onNode(node) },
                    onLongClick = { onNode(node) },
                )
            }
        }
    }
}
