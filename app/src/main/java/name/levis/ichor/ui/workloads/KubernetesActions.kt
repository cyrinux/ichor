package name.levis.ichor.ui.workloads

import androidx.compose.material.icons.outlined.Storage
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Category
import androidx.compose.material.icons.outlined.Dns
import androidx.compose.material.icons.outlined.HealthAndSafety
import androidx.compose.material.icons.outlined.Inventory2
import androidx.compose.material.icons.outlined.MonitorHeart
import androidx.compose.material.icons.outlined.Policy
import androidx.compose.material.icons.outlined.Share
import androidx.compose.material.icons.outlined.Stream
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import name.levis.ichor.R
import name.levis.ichor.model.KubernetesAction
import name.levis.ichor.ui.components.ActionLook

private fun kubernetesActionIcon(action: KubernetesAction): ImageVector = when (action) {
    KubernetesAction.CHECKUP -> Icons.Outlined.HealthAndSafety
    KubernetesAction.NETWORK_POLICIES -> Icons.Outlined.Policy
    KubernetesAction.SHARE -> Icons.Outlined.Share
    KubernetesAction.API_HEALTH -> Icons.Outlined.MonitorHeart
    KubernetesAction.FLOWS -> Icons.Outlined.Stream
    KubernetesAction.RESOURCES -> Icons.Outlined.Category
    KubernetesAction.HELM -> Icons.Outlined.Inventory2
    KubernetesAction.STORAGE -> Icons.Outlined.Storage
    KubernetesAction.API_ADDRESS -> Icons.Outlined.Dns
}

/** How the Kubernetes screen's actions look, in its bar and its editor. */
val kubernetesActionLook = ActionLook<KubernetesAction>(::kubernetesActionIcon) { action ->
    stringResource(
        when (action) {
            KubernetesAction.CHECKUP -> R.string.checkup_title
            KubernetesAction.NETWORK_POLICIES -> R.string.netpol_title
            KubernetesAction.SHARE -> R.string.share_link
            KubernetesAction.API_HEALTH -> R.string.apihealth_title
            KubernetesAction.FLOWS -> R.string.flows_title
            KubernetesAction.RESOURCES -> R.string.kb_title
            KubernetesAction.HELM -> R.string.kb_helm_title
            KubernetesAction.STORAGE -> R.string.storage_title
            KubernetesAction.API_ADDRESS -> R.string.kube_server_title
        },
    )
}
