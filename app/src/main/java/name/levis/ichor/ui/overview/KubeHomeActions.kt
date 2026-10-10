package name.levis.ichor.ui.overview

import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Category
import androidx.compose.material.icons.outlined.HealthAndSafety
import androidx.compose.material.icons.outlined.Inventory2
import androidx.compose.material.icons.outlined.MonitorHeart
import androidx.compose.material.icons.outlined.NotificationsActive
import androidx.compose.material.icons.outlined.Policy
import androidx.compose.material.icons.outlined.QueryStats
import androidx.compose.material.icons.outlined.Settings
import androidx.compose.material.icons.outlined.Storage
import androidx.compose.material.icons.outlined.Sync
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
    KubeHomeAction.GITOPS -> Icons.Outlined.Sync
    KubeHomeAction.METRICS -> Icons.Outlined.QueryStats
    KubeHomeAction.ALERTS -> Icons.Outlined.NotificationsActive
    KubeHomeAction.HELM -> Icons.Outlined.Inventory2
    KubeHomeAction.DATA_SERVICES -> Icons.Outlined.Storage
    KubeHomeAction.CHECKUP -> Icons.Outlined.HealthAndSafety
    KubeHomeAction.API_HEALTH -> Icons.Outlined.MonitorHeart
    KubeHomeAction.NETWORK_POLICIES -> Icons.Outlined.Policy
    KubeHomeAction.SETTINGS -> Icons.Outlined.Settings
}

@Composable
fun kubeHomeActionLabel(action: KubeHomeAction): String = when (action) {
    KubeHomeAction.WORKLOADS -> stringResource(R.string.overview_action_workloads)
    KubeHomeAction.RESOURCES -> stringResource(R.string.kb_title)
    KubeHomeAction.GITOPS -> "GitOps"
    KubeHomeAction.METRICS -> stringResource(R.string.metrics_title)
    KubeHomeAction.ALERTS -> stringResource(R.string.alerts_title)
    KubeHomeAction.HELM -> stringResource(R.string.kb_helm_title)
    KubeHomeAction.DATA_SERVICES -> stringResource(R.string.data_services_title)
    KubeHomeAction.CHECKUP -> stringResource(R.string.checkup_title)
    KubeHomeAction.API_HEALTH -> stringResource(R.string.apihealth_title)
    KubeHomeAction.NETWORK_POLICIES -> stringResource(R.string.netpol_title)
    KubeHomeAction.SETTINGS -> stringResource(R.string.overview_action_settings)
}

/** How the Kubernetes home's actions look, in its bar and its editor. */
val kubeHomeActionLook = ActionLook<KubeHomeAction>(::kubeHomeActionIcon) { kubeHomeActionLabel(it) }

/** Where the Kubernetes home's app-bar actions lead; GitOps to Flux when [flux], else Argo CD. */
fun KubeHomeNavigation.open(action: KubeHomeAction, flux: Boolean = false): () -> Unit = when (action) {
    KubeHomeAction.WORKLOADS -> onWorkloads
    KubeHomeAction.RESOURCES -> onResources
    KubeHomeAction.GITOPS -> if (flux) onFlux else onArgoCD
    KubeHomeAction.METRICS -> onMetrics
    KubeHomeAction.ALERTS -> onAlerts
    KubeHomeAction.HELM -> onHelm
    KubeHomeAction.DATA_SERVICES -> { { onDataServices(null) } }
    KubeHomeAction.CHECKUP -> onCheckup
    KubeHomeAction.API_HEALTH -> onApiHealth
    KubeHomeAction.NETWORK_POLICIES -> onNetworkPolicies
    KubeHomeAction.SETTINGS -> onSettings
}

/**
 * The Kubernetes home's app-bar actions as arranged in [bar]: its icons, then the rest behind a
 * menu, so the cluster name keeps room on a phone. The menu also leads to arranging them.
 * GitOps is only offered once the cluster answered it runs Argo CD ([argo]) or Flux ([flux]).
 */
@Composable
fun KubeHomeActions(bar: KubeHomeBar, nav: KubeHomeNavigation, argo: Boolean, flux: Boolean, onCustomize: () -> Unit) {
    ActionBarActions(
        bar = bar,
        look = kubeHomeActionLook,
        onClick = { nav.open(it, flux = !argo)() },
        customizeLabel = stringResource(R.string.kube_home_edit_title),
        onCustomize = onCustomize,
        offered = { it != KubeHomeAction.GITOPS || argo || flux },
    )
}
