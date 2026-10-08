package name.levis.ichor.ui.overview

import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Favorite
import androidx.compose.material.icons.outlined.Hub
import androidx.compose.material.icons.outlined.Storage
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
import name.levis.ichor.ui.components.Destination
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

/** The screen an action shares with the other bars (Destination), null for the Talos-only ones. */
private val OverviewAction.destination: Destination?
    get() = when (this) {
        OverviewAction.EVENTS -> Destination.EVENTS
        OverviewAction.WORKLOADS -> Destination.WORKLOADS
        OverviewAction.METRICS -> Destination.METRICS
        OverviewAction.SETTINGS -> Destination.SETTINGS
        OverviewAction.HEALTH, OverviewAction.KUBESPAN, OverviewAction.ETCD -> null
    }

fun overviewActionIcon(action: OverviewAction): ImageVector = action.destination?.icon ?: when (action) {
    OverviewAction.HEALTH -> Icons.Outlined.Favorite
    OverviewAction.KUBESPAN -> Icons.Outlined.Hub
    else -> Icons.Outlined.Storage // etcd
}

@Composable
fun overviewActionLabel(action: OverviewAction): String = action.destination?.let { stringResource(it.label) } ?: when (action) {
    OverviewAction.HEALTH -> stringResource(R.string.overview_action_health)
    OverviewAction.KUBESPAN -> "KubeSpan"
    else -> "etcd"
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
