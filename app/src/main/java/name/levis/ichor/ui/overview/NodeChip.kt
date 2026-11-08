package name.levis.ichor.ui.overview

import androidx.compose.foundation.background
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.CustomAccessibilityAction
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.customActions
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.NodeStatus

/**
 * A node in a collapsed Nodes card, Talos or Kubernetes: its status dot and [name], the whole
 * chip tinted when it has a problem since no row spells it out. Tap is [onClick], a long press
 * [onLongClick] (its actions); [onLive] offers the live graphs to TalkBack where there are some.
 */
@Composable
internal fun NodeStatusChip(
    name: String,
    status: NodeStatus,
    cordoned: Boolean,
    onClick: () -> Unit,
    onLongClick: () -> Unit,
    onLive: (() -> Unit)? = null,
) {
    val liveLabel = stringResource(R.string.overview_action_live_graphs)
    val label = statusLabel(status)
    val color = statusColor(status)
    val calm = status == NodeStatus.READY
    Surface(
        shape = RoundedCornerShape(50),
        color = if (calm) MaterialTheme.colorScheme.surfaceVariant else color.copy(alpha = 0.16f),
        contentColor = if (calm) MaterialTheme.colorScheme.onSurfaceVariant else color,
        modifier = Modifier.combinedClickable(
            onClick = onClick,
            onLongClick = onLongClick,
            onLongClickLabel = stringResource(R.string.overview_node_actions),
        ).semantics(mergeDescendants = true) {
            contentDescription = "$name, $label"
            if (onLive != null) customActions = listOf(CustomAccessibilityAction(liveLabel) { onLive(); true })
        },
    ) {
        Row(Modifier.padding(horizontal = 12.dp, vertical = 6.dp), verticalAlignment = Alignment.CenterVertically) {
            Box(Modifier.size(8.dp).background(color, CircleShape))
            Spacer(Modifier.width(6.dp))
            Text(name, style = MaterialTheme.typography.labelLarge, maxLines = 1, overflow = TextOverflow.Ellipsis)
            if (cordoned) {
                Text(" · " + stringResource(R.string.kube_node_cordoned), style = MaterialTheme.typography.labelLarge, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
        }
    }
}
