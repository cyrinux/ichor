package name.levis.ichor.ui.workloads

import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Dns
import androidx.compose.material.icons.outlined.Share
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import name.levis.ichor.R
import name.levis.ichor.model.KubernetesAction
import name.levis.ichor.ui.components.ActionLook
import name.levis.ichor.ui.components.Destination

/** The screen an action shares with the homes' bars (Destination), null for this screen's own. */
private val KubernetesAction.destination: Destination?
    get() = when (this) {
        KubernetesAction.CHECKUP -> Destination.CHECKUP
        KubernetesAction.NETWORK_POLICIES -> Destination.NETWORK_POLICIES
        KubernetesAction.API_HEALTH -> Destination.API_HEALTH
        KubernetesAction.FLOWS -> Destination.FLOWS
        KubernetesAction.RESOURCES -> Destination.RESOURCES
        KubernetesAction.HELM -> Destination.HELM
        KubernetesAction.SHARE, KubernetesAction.API_ADDRESS -> null
    }

private fun kubernetesActionIcon(action: KubernetesAction): ImageVector = action.destination?.icon ?: when (action) {
    KubernetesAction.SHARE -> Icons.Outlined.Share
    else -> Icons.Outlined.Dns // the API address
}

@Composable
private fun kubernetesActionLabel(action: KubernetesAction): String = stringResource(
    action.destination?.label ?: when (action) {
        KubernetesAction.SHARE -> R.string.share_link
        else -> R.string.kube_server_title
    },
)

/** How the Kubernetes screen's actions look, in its bar and its editor. */
val kubernetesActionLook = ActionLook<KubernetesAction>(::kubernetesActionIcon) { kubernetesActionLabel(it) }
