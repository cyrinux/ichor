package name.levis.ichor.ui.overview

import androidx.compose.foundation.layout.Box
import androidx.compose.material.icons.Icons
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
)

/**
 * The overview's app-bar actions: the most used as icons, the rest behind a menu,
 * so the cluster name keeps room on a phone.
 */
@Composable
fun OverviewActions(
    nav: OverviewNavigation,
    reachable: List<String>?,
    health: Boolean,
    workloads: Boolean,
) {
    // Cluster-wide screens: only disabled when no reachable node's Talos has them.
    val features = rememberClusterFeatures(reachable)
    // Only offered when the config's role can run it.
    if (health) TooltipIconButton(Icons.Outlined.Favorite, stringResource(R.string.overview_action_health), onClick = nav.onHealth)
    TooltipIconButton(
        Icons.Outlined.Timeline,
        stringResource(R.string.overview_action_events),
        onClick = nav.onEvents,
        enabled = clusterSupport(features, TalosFeature.EVENTS).supported,
    )
    // Kubernetes workloads: the API is reached with the admin kubeconfig Talos issues.
    if (workloads) TooltipIconButton(Icons.Outlined.Widgets, stringResource(R.string.overview_action_workloads), onClick = nav.onWorkloads)
    var open by remember { mutableStateOf(false) }
    Box {
        TooltipIconButton(Icons.Outlined.MoreVert, stringResource(R.string.common_more), onClick = { open = true })
        DropdownMenu(expanded = open, onDismissRequest = { open = false }) {
            val close = { open = false }
            // PromQL panels, through the same kubeconfig (or a URL set on the screen).
            if (workloads) MenuAction(Icons.Outlined.QueryStats, stringResource(R.string.metrics_title), close, nav.onMetrics)
            MenuAction(Icons.Outlined.Hub, "KubeSpan", close, nav.onKubeSpan, clusterSupport(features, TalosFeature.KUBESPAN).supported)
            MenuAction(Icons.Outlined.Storage, "etcd", close, nav.onEtcd, clusterSupport(features, TalosFeature.ETCD).supported)
            MenuAction(Icons.Outlined.Settings, stringResource(R.string.overview_action_settings), close, nav.onSettings)
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
