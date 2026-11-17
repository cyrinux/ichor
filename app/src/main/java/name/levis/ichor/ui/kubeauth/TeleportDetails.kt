package name.levis.ichor.ui.kubeauth

import androidx.compose.foundation.layout.Column
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.semantics
import name.levis.ichor.R
import name.levis.ichor.model.KubeSignInInfo
import name.levis.ichor.model.TELEPORT_VALUES
import name.levis.ichor.ui.components.InfoRow
import name.levis.ichor.ui.components.MutedText

/**
 * What a Teleport sign-in goes to (the proxy, the Teleport and Kubernetes clusters, the SSO
 * connector last used), read-only, how it works, and how it stands when signed in.
 */
@Composable
internal fun TeleportDetails(info: KubeSignInInfo) {
    TELEPORT_VALUES.forEach { (key, label) ->
        info.values[key]?.takeIf { it.isNotEmpty() }?.let { value ->
            // One stop for TalkBack: the label and its value together.
            Column(Modifier.semantics(mergeDescendants = true) {}) { InfoRow(stringResource(label), value, mono = true) }
        }
    }
    if (info.signedIn) MutedText(signInStatus(info))
    MutedText(stringResource(R.string.kube_signin_teleport_explain))
}
