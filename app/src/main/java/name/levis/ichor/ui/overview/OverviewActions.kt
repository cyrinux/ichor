package name.levis.ichor.ui.overview

import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Favorite
import androidx.compose.material.icons.outlined.Hub
import androidx.compose.material.icons.outlined.QueryStats
import androidx.compose.material.icons.outlined.Settings
import androidx.compose.material.icons.outlined.Storage
import androidx.compose.material.icons.outlined.Sync
import androidx.compose.material.icons.outlined.Timeline
import androidx.compose.material.icons.outlined.Widgets
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import name.levis.ichor.R
import name.levis.ichor.model.OverviewAction
import name.levis.ichor.model.OverviewBar
import name.levis.ichor.model.TalosFeature
import name.levis.ichor.model.clusterSupport
import name.levis.ichor.ui.components.ActionBarActions
import name.levis.ichor.ui.components.ActionLook
import name.levis.ichor.ui.components.rememberClusterFeatures

/** Where the overview's app-bar actions lead. */
class OverviewNavigation(
    val onHealth: () -> Unit,
    val onEvents: () -> Unit,
    val onWorkloads: () -> Unit,
    /** Argo CD when the cluster runs it, else Flux. */
    val onGitOps: () -> Unit,
    val onMetrics: () -> Unit,
    val onKubeSpan: () -> Unit,
    val onEtcd: () -> Unit,
    val onSettings: () -> Unit,
) {
    fun open(action: OverviewAction): () -> Unit = when (action) {
        OverviewAction.HEALTH -> onHealth
        OverviewAction.EVENTS -> onEvents
        OverviewAction.WORKLOADS -> onWorkloads
        OverviewAction.GITOPS -> onGitOps
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
    OverviewAction.GITOPS -> Icons.Outlined.Sync
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
    OverviewAction.GITOPS -> "GitOps"
    OverviewAction.METRICS -> stringResource(R.string.metrics_title)
    OverviewAction.KUBESPAN -> "KubeSpan"
    OverviewAction.ETCD -> "etcd"
    OverviewAction.SETTINGS -> stringResource(R.string.overview_action_settings)
}

/** How the overview's actions look, in its bar and its editor. */
val overviewActionLook = ActionLook<OverviewAction>(::overviewActionIcon) { overviewActionLabel(it) }

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
    gitOps: Boolean,
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
    // API with the admin kubeconfig Talos issues; GitOps only when the cluster runs Argo CD or Flux.
    fun offered(action: OverviewAction): Boolean = when (action) {
        OverviewAction.HEALTH -> health
        OverviewAction.WORKLOADS, OverviewAction.METRICS -> workloads
        OverviewAction.GITOPS -> gitOps
        else -> true
    }
    ActionBarActions(
        bar = bar,
        look = overviewActionLook,
        onClick = { nav.open(it)() },
        customizeLabel = stringResource(R.string.overview_edit_title),
        onCustomize = onCustomize,
        offered = ::offered,
        enabled = ::enabled,
    )
}
