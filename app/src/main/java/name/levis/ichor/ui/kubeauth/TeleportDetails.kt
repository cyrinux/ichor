package name.levis.ichor.ui.kubeauth

import androidx.compose.foundation.layout.Column
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.semantics
import name.levis.ichor.R
import name.levis.ichor.model.KubeSignInInfo
import name.levis.ichor.model.METHOD_TELEPORT
import name.levis.ichor.model.TELEPORT_USER
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
    if (info.signedIn) {
        MutedText(signInStatus(info))
    } else {
        // Signed in at the proxy for another of its clusters: this one only needs its certificate.
        info.values[TELEPORT_USER]?.takeIf { it.isNotEmpty() }?.let { MutedText(stringResource(R.string.kube_signin_teleport_signed_in_at_proxy, it)) }
    }
    MutedText(stringResource(R.string.kube_signin_teleport_explain))
}

/** The browser sign-in button: "Get a certificate for …" when signed in at the Teleport proxy already. */
@Composable
internal fun browserSignInText(info: KubeSignInInfo): String {
    val kubeCluster = info.values["kubeCluster"].orEmpty()
    return if (info.method == METHOD_TELEPORT && !info.signedIn && !info.values[TELEPORT_USER].isNullOrEmpty() && kubeCluster.isNotEmpty()) {
        stringResource(R.string.kube_signin_teleport_get_cert, kubeCluster)
    } else {
        stringResource(R.string.kube_signin_action)
    }
}
