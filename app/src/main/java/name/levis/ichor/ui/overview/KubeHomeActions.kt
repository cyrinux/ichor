package name.levis.ichor.ui.overview

import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Category
import androidx.compose.material.icons.outlined.HealthAndSafety
import androidx.compose.material.icons.outlined.Inventory2
import androidx.compose.material.icons.outlined.MonitorHeart
import androidx.compose.material.icons.outlined.Policy
import androidx.compose.material.icons.outlined.QueryStats
import androidx.compose.material.icons.outlined.Settings
import androidx.compose.material.icons.outlined.Storage
import androidx.compose.material.icons.outlined.Timeline
import androidx.compose.material.icons.outlined.Widgets
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import name.levis.ichor.R
import name.levis.ichor.model.KubeHomeAction
import name.levis.ichor.model.KubeHomeBar
import name.levis.ichor.ui.components.ActionBarActions
import name.levis.ichor.ui.components.ActionLook

private fun kubeHomeActionIcon(action: KubeHomeAction): ImageVector = when (action) {
    KubeHomeAction.WORKLOADS -> Icons.Outlined.Widgets
    KubeHomeAction.RESOURCES -> Icons.Outlined.Category
    KubeHomeAction.METRICS -> Icons.Outlined.QueryStats
    KubeHomeAction.HELM -> Icons.Outlined.Inventory2
    KubeHomeAction.DATA_SERVICES -> Icons.Outlined.Storage
    KubeHomeAction.CHECKUP -> Icons.Outlined.HealthAndSafety
    KubeHomeAction.API_HEALTH -> Icons.Outlined.MonitorHeart
    KubeHomeAction.NETWORK_POLICIES -> Icons.Outlined.Policy
    KubeHomeAction.EVENTS -> Icons.Outlined.Timeline
    KubeHomeAction.SETTINGS -> Icons.Outlined.Settings
}

@Composable
fun kubeHomeActionLabel(action: KubeHomeAction): String = stringResource(
    when (action) {
        KubeHomeAction.WORKLOADS -> R.string.overview_action_workloads
        KubeHomeAction.RESOURCES -> R.string.kb_title
        KubeHomeAction.METRICS -> R.string.metrics_title
        KubeHomeAction.HELM -> R.string.kb_helm_title
        KubeHomeAction.DATA_SERVICES -> R.string.data_services_title
        KubeHomeAction.CHECKUP -> R.string.checkup_title
        KubeHomeAction.API_HEALTH -> R.string.apihealth_title
        KubeHomeAction.NETWORK_POLICIES -> R.string.netpol_title
        KubeHomeAction.EVENTS -> R.string.overview_action_events
        KubeHomeAction.SETTINGS -> R.string.overview_action_settings
    },
)

/** How the Kubernetes home's actions look, in its bar and its editor. */
val kubeHomeActionLook = ActionLook<KubeHomeAction>(::kubeHomeActionIcon) { kubeHomeActionLabel(it) }

/** Where the Kubernetes home's app-bar actions lead. */
fun KubeHomeNavigation.open(action: KubeHomeAction): () -> Unit = when (action) {
    KubeHomeAction.WORKLOADS -> onWorkloads
    KubeHomeAction.RESOURCES -> onResources
    KubeHomeAction.METRICS -> onMetrics
    KubeHomeAction.HELM -> onHelm
    KubeHomeAction.DATA_SERVICES -> { { onDataServices(null) } }
    KubeHomeAction.CHECKUP -> onCheckup
    KubeHomeAction.API_HEALTH -> onApiHealth
    KubeHomeAction.NETWORK_POLICIES -> onNetworkPolicies
    KubeHomeAction.EVENTS -> onEvents
    KubeHomeAction.SETTINGS -> onSettings
}

/**
 * The Kubernetes home's app-bar actions as arranged in [bar]: its icons, then the rest behind a
 * menu, so the cluster name keeps room on a phone. The menu also leads to arranging them.
 */
@Composable
fun KubeHomeActions(bar: KubeHomeBar, nav: KubeHomeNavigation, onCustomize: () -> Unit) {
    ActionBarActions(
        bar = bar,
        look = kubeHomeActionLook,
        onClick = { nav.open(it)() },
        customizeLabel = stringResource(R.string.kube_home_edit_title),
        onCustomize = onCustomize,
    )
}
