package name.levis.ichor.ui.overview

import androidx.compose.ui.res.stringResource
import name.levis.ichor.R
import androidx.compose.foundation.clickable
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material.icons.automirrored.outlined.ListAlt
import androidx.compose.material.icons.automirrored.outlined.Logout
import androidx.compose.material.icons.outlined.Power
import androidx.compose.material.icons.outlined.PowerSettingsNew
import androidx.compose.material.icons.outlined.SettingsEthernet
import androidx.compose.material.icons.automirrored.outlined.ShowChart
import androidx.compose.material.icons.outlined.Terminal
import androidx.compose.material.icons.outlined.MoreHoriz
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.ListItem
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.SwipeToDismissBox
import androidx.compose.material3.SwipeToDismissBoxValue
import androidx.compose.material3.Text
import androidx.compose.material3.rememberSwipeToDismissBoxState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch
import name.levis.ichor.model.NodeOverview
import name.levis.ichor.model.TalosFeature
import name.levis.ichor.model.WolTarget
import name.levis.ichor.model.notice
import name.levis.ichor.model.support
import name.levis.ichor.ui.components.copyToClipboard
import name.levis.ichor.ui.components.rememberNodeFeatures
import name.levis.ichor.ui.components.text
import name.levis.ichor.ui.theme.LocalStatusColors

/** What a node row can lead to; destructive ones only ever open their confirmation. */
enum class NodeAction { LIVE, SERVICES, KERNEL_LOG, REBOOT, SHUTDOWN, SHELL, DRAIN }

/**
 * Swipe right: the node's live graphs. Swipe left (or long-press the card): the action sheet.
 * The card always springs back; nothing runs on the swipe itself.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SwipeableNode(
    node: NodeOverview,
    onLive: () -> Unit,
    onMore: () -> Unit,
    content: @Composable () -> Unit,
) {
    val state = rememberSwipeToDismissBoxState()
    val scope = rememberCoroutineScope()
    SwipeToDismissBox(
        state = state,
        enableDismissFromStartToEnd = node.reachable, // no live data from an unreachable node
        onDismiss = { value ->
            when (value) {
                SwipeToDismissBoxValue.StartToEnd -> onLive()
                SwipeToDismissBoxValue.EndToStart -> onMore()
                SwipeToDismissBoxValue.Settled -> Unit
            }
            scope.launch { state.reset() } // spring back
        },
        backgroundContent = {
            val toLive = state.dismissDirection == SwipeToDismissBoxValue.StartToEnd
            Row(
                Modifier.fillMaxSize().clip(RoundedCornerShape(12.dp))
                    .background(MaterialTheme.colorScheme.secondaryContainer)
                    .padding(horizontal = 24.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = if (toLive) Arrangement.Start else Arrangement.End,
            ) {
                val (icon, label) = if (toLive) {
                    Icons.AutoMirrored.Outlined.ShowChart to stringResource(R.string.overview_swipe_live)
                } else {
                    Icons.Outlined.MoreHoriz to stringResource(R.string.overview_swipe_actions)
                }
                Icon(icon, contentDescription = null, tint = MaterialTheme.colorScheme.onSecondaryContainer)
                Spacer(Modifier.width(8.dp))
                Text(label, color = MaterialTheme.colorScheme.onSecondaryContainer)
            }
        },
    ) { content() }
}

/** How the actions sheet offers Wake-on-LAN for a node; null when it does not (screenshot mode). */
data class WolActions(
    /** Where "Wake" sends magic packets (see wakeTargets); empty: nothing to wake it with yet. */
    val targets: List<WolTarget>,
    val onWake: () -> Unit,
    val onSettings: () -> Unit,
)

/**
 * Per-node actions, filtered by reachability and the config's role. Wake-on-LAN needs no
 * role: the phone sends the packet itself, the Talos API is not involved. [canDrain]: the
 * drain goes through the Kubernetes API (os:admin).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun NodeActionsSheet(
    node: NodeOverview,
    canPower: Boolean,
    canShell: Boolean,
    wol: WolActions?,
    canDrain: Boolean = false,
    onAction: (NodeAction) -> Unit,
    onDismiss: () -> Unit,
) {
    val context = LocalContext.current
    val shell = rememberNodeFeatures(node.node).support(TalosFeature.DEBUG_SHELL).notice
    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(Modifier.padding(bottom = 24.dp)) {
            Text(
                node.hostname,
                style = MaterialTheme.typography.titleMedium,
                modifier = Modifier.padding(horizontal = 24.dp, vertical = 8.dp),
            )
            fun pick(action: NodeAction) {
                onDismiss()
                onAction(action)
            }
            if (node.reachable) {
                Item(Icons.AutoMirrored.Outlined.ShowChart, stringResource(R.string.overview_action_live_graphs)) { pick(NodeAction.LIVE) }
                Item(Icons.AutoMirrored.Outlined.ListAlt, stringResource(R.string.overview_action_services_logs)) { pick(NodeAction.SERVICES) }
                Item(Icons.Outlined.Terminal, stringResource(R.string.overview_action_kernel_log)) { pick(NodeAction.KERNEL_LOG) }
                if (canShell) {
                    Item(Icons.Outlined.Terminal, stringResource(R.string.overview_action_debug_shell), disabled = shell?.text()) { pick(NodeAction.SHELL) }
                }
                if (canDrain) {
                    Item(Icons.AutoMirrored.Outlined.Logout, stringResource(R.string.node_menu_drain)) { pick(NodeAction.DRAIN) }
                }
                if (canPower) {
                    Item(Icons.Outlined.PowerSettingsNew, stringResource(R.string.overview_action_reboot), danger = true) { pick(NodeAction.REBOOT) }
                    Item(Icons.Outlined.PowerSettingsNew, stringResource(R.string.overview_action_shutdown), danger = true) { pick(NodeAction.SHUTDOWN) }
                }
            } else {
                Text(
                    node.error ?: stringResource(R.string.common_status_unreachable),
                    style = MaterialTheme.typography.bodySmall,
                    color = LocalStatusColors.current.bad,
                    modifier = Modifier.padding(horizontal = 24.dp, vertical = 4.dp),
                )
            }
            wol?.let {
                if (it.targets.isNotEmpty()) {
                    Item(Icons.Outlined.Power, stringResource(R.string.wol_wake)) {
                        onDismiss()
                        it.onWake()
                    }
                }
                Item(Icons.Outlined.SettingsEthernet, stringResource(R.string.wol_settings)) {
                    onDismiss()
                    it.onSettings()
                }
            }
            Item(Icons.Outlined.ContentCopy, stringResource(R.string.overview_action_copy_ip, node.node)) {
                copyToClipboard(context, context.getString(R.string.overview_clip_label), node.node)
                onDismiss()
            }
        }
    }
}

@Composable
private fun Item(icon: ImageVector, label: String, danger: Boolean = false, disabled: String? = null, onClick: () -> Unit) {
    val color = when {
        disabled != null -> MaterialTheme.colorScheme.onSurface.copy(alpha = 0.38f)
        danger -> LocalStatusColors.current.bad
        else -> MaterialTheme.colorScheme.onSurface
    }
    ListItem(
        headlineContent = { Text(label, color = color) },
        // [disabled]: why the action is unavailable (e.g. "Needs Talos v1.x or newer").
        supportingContent = disabled?.let { { Text(it) } },
        leadingContent = { Icon(icon, contentDescription = null, tint = color) },
        modifier = Modifier.fillMaxWidth().clickable(enabled = disabled == null, onClick = onClick),
    )
}

