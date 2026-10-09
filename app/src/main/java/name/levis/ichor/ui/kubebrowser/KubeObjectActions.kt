package name.levis.ichor.ui.kubebrowser

import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.Edit
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material.icons.outlined.SettingsEthernet
import androidx.compose.material.icons.outlined.Share
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import name.levis.ichor.R
import name.levis.ichor.model.KubeObjectAction
import name.levis.ichor.ui.components.ActionLook

private fun kubeObjectActionIcon(action: KubeObjectAction): ImageVector = when (action) {
    KubeObjectAction.EDIT -> Icons.Outlined.Edit
    KubeObjectAction.REFRESH -> Icons.Outlined.Refresh
    KubeObjectAction.COPY -> Icons.Outlined.ContentCopy
    KubeObjectAction.SHARE -> Icons.Outlined.Share
    KubeObjectAction.PORT_FORWARD -> Icons.Outlined.SettingsEthernet
    KubeObjectAction.DELETE -> Icons.Outlined.Delete
}

/** How a Kubernetes object's actions look, in its bar and its editor. */
val kubeObjectActionLook = ActionLook<KubeObjectAction>(::kubeObjectActionIcon) { action ->
    stringResource(
        when (action) {
            KubeObjectAction.EDIT -> R.string.kb_edit
            KubeObjectAction.REFRESH -> R.string.common_refresh
            KubeObjectAction.COPY -> R.string.kb_copy
            KubeObjectAction.SHARE -> R.string.kb_share
            KubeObjectAction.PORT_FORWARD -> R.string.kb_forward_title
            KubeObjectAction.DELETE -> R.string.kb_delete
        },
    )
}
