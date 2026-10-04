package name.levis.ichor.ui.overview

import androidx.compose.foundation.layout.Box
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Edit
import androidx.compose.material.icons.outlined.Favorite
import androidx.compose.material.icons.outlined.Hub
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material.icons.outlined.QueryStats
import androidx.compose.material.icons.outlined.Settings
import androidx.compose.material.icons.outlined.Storage
import androidx.compose.material.icons.outlined.Timeline
import androidx.compose.material.icons.outlined.Widgets
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import name.levis.ichor.R
import name.levis.ichor.model.OverviewAction
import name.levis.ichor.model.OverviewBar
import name.levis.ichor.model.TalosFeature
import name.levis.ichor.model.clusterSupport
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.rememberClusterFeatures

/** Where the overview's app-bar actions lead. */
class OverviewNavigation(
    val onHealth: () -> Unit,
    val onEvents: () -> Unit,
    val onWorkloads: () -> Unit,
    val onMetrics: () -> Unit,
    val onKubeSpan: () -> Unit,
    val onEtcd: () -> Unit,
    val onSettings: () -> Unit,
) {
    fun open(action: OverviewAction): () -> Unit = when (action) {
        OverviewAction.HEALTH -> onHealth
        OverviewAction.EVENTS -> onEvents
        OverviewAction.WORKLOADS -> onWorkloads
        OverviewAction.METRICS -> onMetrics
        OverviewAction.KUBESPAN -> onKubeSpan
        OverviewAction.ETCD -> onEtcd
        OverviewAction.SETTINGS -> onSettings
    }
}

fun overviewActionIcon(action: OverviewAction): ImageVector = when (action) {
    OverviewAction.HEALTH -> Icons.Outlined.Favorite
    OverviewAction.EVENTS -> Icons.Outlined.Timeline
    OverviewAction.WORKLOADS -> Icons.Outlined.Widgets
    OverviewAction.METRICS -> Icons.Outlined.QueryStats
    OverviewAction.KUBESPAN -> Icons.Outlined.Hub
    OverviewAction.ETCD -> Icons.Outlined.Storage
    OverviewAction.SETTINGS -> Icons.Outlined.Settings
}

@Composable
fun overviewActionLabel(action: OverviewAction): String = when (action) {
    OverviewAction.HEALTH -> stringResource(R.string.overview_action_health)
    OverviewAction.EVENTS -> stringResource(R.string.overview_action_events)
    OverviewAction.WORKLOADS -> stringResource(R.string.overview_action_workloads)
    OverviewAction.METRICS -> stringResource(R.string.metrics_title)
    OverviewAction.KUBESPAN -> "KubeSpan"
    OverviewAction.ETCD -> "etcd"
    OverviewAction.SETTINGS -> stringResource(R.string.overview_action_settings)
}

/**
 * The overview's app-bar actions as arranged in [bar]: its icons, then the rest behind a menu,
 * so the cluster name keeps room on a phone. The menu also leads to arranging them.
 */
@Composable
fun OverviewActions(
    bar: OverviewBar,
    nav: OverviewNavigation,
    reachable: List<String>?,
    health: Boolean,
    workloads: Boolean,
    onCustomize: () -> Unit,
) {
    // Cluster-wide screens: only disabled when no reachable node's Talos has them.
    val features = rememberClusterFeatures(reachable)
    fun enabled(action: OverviewAction): Boolean = when (action) {
        OverviewAction.EVENTS -> clusterSupport(features, TalosFeature.EVENTS).supported
        OverviewAction.KUBESPAN -> clusterSupport(features, TalosFeature.KUBESPAN).supported
        OverviewAction.ETCD -> clusterSupport(features, TalosFeature.ETCD).supported
        else -> true
    }
    // Only offered when the config's role can run it. Workloads and the PromQL panels reach the
    // API with the admin kubeconfig Talos issues.
    fun offered(action: OverviewAction): Boolean = when (action) {
        OverviewAction.HEALTH -> health
        OverviewAction.WORKLOADS, OverviewAction.METRICS -> workloads
        else -> true
    }
    bar.icons.filter(::offered).forEach { action ->
        TooltipIconButton(overviewActionIcon(action), overviewActionLabel(action), onClick = nav.open(action), enabled = enabled(action))
    }
    var open by remember { mutableStateOf(false) }
    Box {
        TooltipIconButton(Icons.Outlined.MoreVert, stringResource(R.string.common_more), onClick = { open = true })
        DropdownMenu(expanded = open, onDismissRequest = { open = false }) {
            val close = { open = false }
            val menu = bar.menu.filter(::offered)
            menu.forEach { action ->
                MenuAction(overviewActionIcon(action), overviewActionLabel(action), close, nav.open(action), enabled(action))
            }
            if (menu.isNotEmpty()) HorizontalDivider()
            MenuAction(Icons.Outlined.Edit, stringResource(R.string.overview_edit_title), close, onCustomize)
        }
    }
}

@Composable
private fun MenuAction(icon: ImageVector, label: String, close: () -> Unit, onClick: () -> Unit, enabled: Boolean = true) {
    DropdownMenuItem(
        text = { Text(label) },
        leadingIcon = { Icon(icon, contentDescription = null) },
        enabled = enabled,
        onClick = {
            close()
            onClick()
        },
    )
}
