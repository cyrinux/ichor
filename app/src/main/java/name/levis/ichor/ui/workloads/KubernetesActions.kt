package name.levis.ichor.ui.workloads

import androidx.compose.material.icons.outlined.Storage
import androidx.compose.material.icons.outlined.Lan
import androidx.compose.material.icons.outlined.TaskAlt
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.AccountTree
import androidx.compose.material.icons.outlined.Category
import androidx.compose.material.icons.outlined.Computer
import androidx.compose.material.icons.outlined.DataUsage
import androidx.compose.material.icons.outlined.Dataset
import androidx.compose.material.icons.outlined.Extension
import androidx.compose.material.icons.outlined.Gavel
import androidx.compose.material.icons.outlined.HealthAndSafety
import androidx.compose.material.icons.outlined.Inventory2
import androidx.compose.material.icons.outlined.MonitorHeart
import androidx.compose.material.icons.outlined.Policy
import androidx.compose.material.icons.outlined.Share
import androidx.compose.material.icons.outlined.Speed
import androidx.compose.material.icons.outlined.Stream
import androidx.compose.material.icons.outlined.Timeline
import androidx.compose.material.icons.outlined.Upgrade
import androidx.compose.material.icons.outlined.VpnLock
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import name.levis.ichor.R
import name.levis.ichor.model.DataServiceKind
import name.levis.ichor.model.KubernetesAction
import name.levis.ichor.model.KubernetesActionGroup
import name.levis.ichor.model.group
import name.levis.ichor.model.groupedMenu
import name.levis.ichor.model.title
import name.levis.ichor.ui.components.ActionGroups
import name.levis.ichor.ui.components.ActionLook
import name.levis.ichor.ui.dataservices.fallbackIcon

private fun kubernetesActionIcon(action: KubernetesAction): ImageVector = when (action) {
    KubernetesAction.CHECKUP -> Icons.Outlined.HealthAndSafety
    KubernetesAction.NETWORK_POLICIES -> Icons.Outlined.Policy
    KubernetesAction.SHARE -> Icons.Outlined.Share
    KubernetesAction.API_HEALTH -> Icons.Outlined.MonitorHeart
    KubernetesAction.FLOWS -> Icons.Outlined.Stream
    KubernetesAction.RESOURCES -> Icons.Outlined.Category
    KubernetesAction.HELM -> Icons.Outlined.Inventory2
    KubernetesAction.STORAGE -> Icons.Outlined.Storage
    KubernetesAction.SERVICES -> Icons.Outlined.Lan
    KubernetesAction.JOBS -> Icons.Outlined.TaskAlt
    KubernetesAction.EVENTS -> Icons.Outlined.Timeline
    KubernetesAction.POLICIES -> Icons.Outlined.Gavel
    KubernetesAction.AUTOSCALING -> Icons.Outlined.Speed
    KubernetesAction.VIRTUAL_MACHINES -> Icons.Outlined.Computer
    KubernetesAction.CAPACITY -> Icons.Outlined.DataUsage
    KubernetesAction.CROSSPLANE -> Icons.Outlined.Extension
    KubernetesAction.TAILSCALE -> Icons.Outlined.VpnLock
    KubernetesAction.ARGO_ROLLOUTS -> Icons.Outlined.Upgrade
    KubernetesAction.CLUSTER_LIFECYCLE -> Icons.Outlined.AccountTree
    KubernetesAction.DATA_SERVICES -> Icons.Outlined.Dataset
    KubernetesAction.CERT_MANAGER, KubernetesAction.CASTAI ->
        action.dataServiceKind!!.fallbackIcon
}

/** The system read with Data services an action opens alone; null for the others. */
val KubernetesAction.dataServiceKind: DataServiceKind?
    get() = when (this) {
        KubernetesAction.CERT_MANAGER -> DataServiceKind.CERT_MANAGER
        KubernetesAction.CASTAI -> DataServiceKind.CASTAI
        else -> null
    }

private fun KubernetesActionGroup.label(): Int = when (this) {
    KubernetesActionGroup.EXPLORE -> R.string.kube_menu_explore
    KubernetesActionGroup.HEALTH -> R.string.kube_menu_health
    KubernetesActionGroup.INSTALLED -> R.string.kube_menu_installed
}

/** The Kubernetes screen's menu in sections (Explore, Health, Installed here), in its bar and its editor. */
val kubernetesActionGroups = ActionGroups<KubernetesAction>(
    label = { it.group.label() },
    sections = { menu -> groupedMenu(menu).map { (group, actions) -> group.label() to actions } },
)

/** How the Kubernetes screen's actions look, in its bar and its editor. */
val kubernetesActionLook = ActionLook<KubernetesAction>(::kubernetesActionIcon) { action ->
    // Product names: never translated.
    action.dataServiceKind?.title ?: stringResource(
        when (action) {
            KubernetesAction.CHECKUP -> R.string.checkup_title
            KubernetesAction.NETWORK_POLICIES -> R.string.netpol_title
            KubernetesAction.SHARE -> R.string.share_link
            KubernetesAction.API_HEALTH -> R.string.apihealth_title
            KubernetesAction.FLOWS -> R.string.flows_title
            KubernetesAction.RESOURCES -> R.string.kb_title
            KubernetesAction.HELM -> R.string.kb_helm_title
            KubernetesAction.STORAGE -> R.string.storage_title
            KubernetesAction.SERVICES -> R.string.kube_services_title
            KubernetesAction.JOBS -> R.string.kube_jobs_title
            KubernetesAction.EVENTS -> R.string.kube_events_stream_title
            KubernetesAction.POLICIES -> R.string.kpol_title
            KubernetesAction.AUTOSCALING -> R.string.autoscaling_title
            KubernetesAction.VIRTUAL_MACHINES -> R.string.vm_title
            KubernetesAction.CAPACITY -> R.string.capacity_title
            KubernetesAction.CROSSPLANE -> R.string.crossplane_title
            KubernetesAction.TAILSCALE -> R.string.tailscale_title
            KubernetesAction.ARGO_ROLLOUTS -> R.string.argo_rollouts_title
            KubernetesAction.CLUSTER_LIFECYCLE -> R.string.cluster_lifecycle_title
            KubernetesAction.DATA_SERVICES -> R.string.data_services_title
            // Named by their product above.
            KubernetesAction.CERT_MANAGER, KubernetesAction.CASTAI -> R.string.data_services_title
        },
    )
}
