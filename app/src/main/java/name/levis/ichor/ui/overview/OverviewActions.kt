package name.levis.ichor.ui.overview

import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Favorite
import androidx.compose.material.icons.outlined.FolderCopy
import androidx.compose.material.icons.outlined.Hub
import androidx.compose.material.icons.outlined.NotificationsActive
import androidx.compose.material.icons.outlined.QueryStats
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material.icons.outlined.Settings
import androidx.compose.material.icons.outlined.Storage
import androidx.compose.material.icons.outlined.Sync
import androidx.compose.material.icons.outlined.Timeline
import androidx.compose.material.icons.outlined.Widgets
import androidx.compose.foundation.layout.Row
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
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
import name.levis.ichor.ui.tour.TourAnchor
import name.levis.ichor.ui.tour.tourAnchor

/** Where the overview's app-bar actions lead. */
class OverviewNavigation(
    val onHealth: () -> Unit,
    val onEvents: () -> Unit,
    val onWorkloads: () -> Unit,
    /** Argo CD when the cluster runs it, else Flux. */
    val onGitOps: () -> Unit,
    val onMetrics: () -> Unit,
    val onAlerts: () -> Unit,
    val onKubeSpan: () -> Unit,
    val onEtcd: () -> Unit,
    val onSettings: () -> Unit,
    /** The search across clusters. */
    val onSearch: () -> Unit,
    /** The cluster's own Git repository, from its root. */
    val onRepository: () -> Unit,
) {
    fun open(action: OverviewAction): () -> Unit = when (action) {
        OverviewAction.HEALTH -> onHealth
        OverviewAction.EVENTS -> onEvents
        OverviewAction.WORKLOADS -> onWorkloads
        OverviewAction.GITOPS -> onGitOps
        OverviewAction.METRICS -> onMetrics
        OverviewAction.ALERTS -> onAlerts
        OverviewAction.KUBESPAN -> onKubeSpan
        OverviewAction.ETCD -> onEtcd
        OverviewAction.SEARCH -> onSearch
        OverviewAction.SETTINGS -> onSettings
        OverviewAction.REPOSITORY -> onRepository
    }
}

fun overviewActionIcon(action: OverviewAction): ImageVector = when (action) {
    OverviewAction.HEALTH -> Icons.Outlined.Favorite
    OverviewAction.EVENTS -> Icons.Outlined.Timeline
    OverviewAction.WORKLOADS -> Icons.Outlined.Widgets
    OverviewAction.GITOPS -> Icons.Outlined.Sync
    OverviewAction.METRICS -> Icons.Outlined.QueryStats
    OverviewAction.ALERTS -> Icons.Outlined.NotificationsActive
    OverviewAction.KUBESPAN -> Icons.Outlined.Hub
    OverviewAction.ETCD -> Icons.Outlined.Storage
    OverviewAction.SEARCH -> Icons.Outlined.Search
    OverviewAction.SETTINGS -> Icons.Outlined.Settings
    OverviewAction.REPOSITORY -> Icons.Outlined.FolderCopy
}

@Composable
fun overviewActionLabel(action: OverviewAction): String = when (action) {
    OverviewAction.HEALTH -> stringResource(R.string.overview_action_health)
    OverviewAction.EVENTS -> stringResource(R.string.overview_action_events)
    OverviewAction.WORKLOADS -> stringResource(R.string.overview_action_workloads)
    OverviewAction.GITOPS -> "GitOps"
    OverviewAction.METRICS -> stringResource(R.string.metrics_title)
    OverviewAction.ALERTS -> stringResource(R.string.alerts_title)
    OverviewAction.KUBESPAN -> "KubeSpan"
    OverviewAction.ETCD -> "etcd"
    OverviewAction.SEARCH -> stringResource(R.string.overview_action_search)
    OverviewAction.SETTINGS -> stringResource(R.string.overview_action_settings)
    OverviewAction.REPOSITORY -> stringResource(R.string.repo_title)
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
    /** A repository is saved for the cluster (the demo has one built in). */
    repository: Boolean = false,
) {
    // Cluster-wide screens: only disabled when no reachable node's Talos has them.
    val features = rememberClusterFeatures(reachable)
    fun enabled(action: OverviewAction): Boolean = when (action) {
        OverviewAction.EVENTS -> clusterSupport(features, TalosFeature.EVENTS).supported
        OverviewAction.KUBESPAN -> clusterSupport(features, TalosFeature.KUBESPAN).supported
        OverviewAction.ETCD -> clusterSupport(features, TalosFeature.ETCD).supported
        OverviewAction.REPOSITORY -> repository
        else -> true
    }
    // Only offered when the config's role can run it. Workloads, the PromQL panels and the alerts reach the
    // API with the admin kubeconfig Talos issues; GitOps only when the cluster runs Argo CD or Flux.
    fun offered(action: OverviewAction): Boolean = when (action) {
        OverviewAction.HEALTH -> health
        OverviewAction.WORKLOADS, OverviewAction.METRICS, OverviewAction.ALERTS -> workloads
        OverviewAction.GITOPS -> gitOps
        else -> true
    }
    Row(Modifier.tourAnchor(TourAnchor.Actions)) {
        ActionBarActions(
            bar = bar,
            look = overviewActionLook,
            onClick = { nav.open(it)() },
            customizeLabel = stringResource(R.string.overview_edit_title),
            onCustomize = onCustomize,
            offered = ::offered,
            enabled = ::enabled,
            marker = { if (it == OverviewAction.SETTINGS) Modifier.tourAnchor(TourAnchor.Settings) else Modifier },
        )
    }
}
