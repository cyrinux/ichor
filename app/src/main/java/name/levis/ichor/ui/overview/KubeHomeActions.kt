package name.levis.ichor.ui.overview

import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Category
import androidx.compose.material.icons.outlined.Extension
import androidx.compose.material.icons.outlined.FolderCopy
import androidx.compose.material.icons.outlined.HealthAndSafety
import androidx.compose.material.icons.outlined.Inventory2
import androidx.compose.material.icons.outlined.MonitorHeart
import androidx.compose.material.icons.outlined.NotificationsActive
import androidx.compose.material.icons.outlined.Policy
import androidx.compose.material.icons.outlined.QueryStats
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material.icons.outlined.Settings
import androidx.compose.material.icons.outlined.VpnLock
import androidx.compose.material.icons.outlined.AccountTree
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
    KubeHomeAction.SEARCH -> Icons.Outlined.Search
    KubeHomeAction.SETTINGS -> Icons.Outlined.Settings
    KubeHomeAction.CROSSPLANE -> Icons.Outlined.Extension
    KubeHomeAction.TAILSCALE -> Icons.Outlined.VpnLock
    KubeHomeAction.CLUSTER_LIFECYCLE -> Icons.Outlined.AccountTree
    KubeHomeAction.REPOSITORY -> Icons.Outlined.FolderCopy
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
    KubeHomeAction.SEARCH -> stringResource(R.string.overview_action_search)
    KubeHomeAction.SETTINGS -> stringResource(R.string.overview_action_settings)
    KubeHomeAction.CROSSPLANE -> stringResource(R.string.crossplane_title)
    KubeHomeAction.TAILSCALE -> stringResource(R.string.tailscale_title)
    KubeHomeAction.CLUSTER_LIFECYCLE -> stringResource(R.string.cluster_lifecycle_title)
    KubeHomeAction.REPOSITORY -> stringResource(R.string.repo_title)
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
    KubeHomeAction.DATA_SERVICES -> { { onDataServices(null, false) } }
    KubeHomeAction.CHECKUP -> onCheckup
    KubeHomeAction.API_HEALTH -> onApiHealth
    KubeHomeAction.NETWORK_POLICIES -> onNetworkPolicies
    KubeHomeAction.SEARCH -> onSearch
    KubeHomeAction.SETTINGS -> onSettings
    KubeHomeAction.CROSSPLANE -> onCrossplane
    KubeHomeAction.TAILSCALE -> onTailscale
    KubeHomeAction.CLUSTER_LIFECYCLE -> onClusterLifecycle
    KubeHomeAction.REPOSITORY -> onRepository
}

/**
 * The Kubernetes home's app-bar actions as arranged in [bar]: its icons, then the rest behind a
 * menu, so the cluster name keeps room on a phone. The menu also leads to arranging them.
 * GitOps is only offered once the cluster answered it runs Argo CD ([argo]) or Flux ([flux]);
 * the repository is enabled once one is saved ([repository]).
 */
@Composable
fun KubeHomeActions(bar: KubeHomeBar, nav: KubeHomeNavigation, argo: Boolean, flux: Boolean, crossplane: Boolean = false, tailscale: Boolean = false, clusterLifecycle: Boolean = false, repository: Boolean = false, onCustomize: () -> Unit) {
    ActionBarActions(
        bar = bar,
        look = kubeHomeActionLook,
        onClick = { nav.open(it, flux = !argo)() },
        customizeLabel = stringResource(R.string.kube_home_edit_title),
        onCustomize = onCustomize,
        enabled = { it != KubeHomeAction.REPOSITORY || repository },
        offered = { (it != KubeHomeAction.GITOPS || argo || flux) && (it != KubeHomeAction.CROSSPLANE || crossplane) &&
            (it != KubeHomeAction.TAILSCALE || tailscale) && (it != KubeHomeAction.CLUSTER_LIFECYCLE || clusterLifecycle)
        },
    )
}
