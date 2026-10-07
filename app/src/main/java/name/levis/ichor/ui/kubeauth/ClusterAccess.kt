package name.levis.ichor.ui.kubeauth

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.selection.selectableGroup
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import name.levis.ichor.data.StoredConfig
import name.levis.ichor.data.activeSummary
import name.levis.ichor.data.signInContextFor
import name.levis.ichor.model.isOmni
import name.levis.ichor.ui.UiText
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.KubeSignInInfo
import name.levis.ichor.model.SignInNeeded
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.userMessage
import name.levis.ichor.util.formatDateTime

/**
 * The cluster needs a sign-in: never signed in, or a call said so ([needed], with the reason
 * the core gave). [onSignIn] opens the sign-in.
 */
@Composable
fun SignInBanner(method: String, needed: SignInNeeded?, onSignIn: () -> Unit) {
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(stringResource(R.string.kube_signin_needed), style = MaterialTheme.typography.titleMedium, color = LocalStatusColors.current.warn)
            Text(stringResource(R.string.kube_signin_needed_body, signInMethodText(method.ifEmpty { needed?.method.orEmpty() })), style = MaterialTheme.typography.bodyMedium)
            needed?.reason?.takeIf { it.isNotEmpty() }?.let { MutedText(it) }
            Button(onClick = onSignIn) { Text(stringResource(R.string.kube_signin_action)) }
        }
    }
}

/**
 * "Sign in" for an error card whose call was refused for want of a sign-in: opens the sign-in
 * of the cluster the call was for (the one on screen, or the kubeconfig cluster a Talos one
 * reaches Kubernetes through; for Omni, the Talos cluster on screen). Nothing when [message] is another error or no cluster can sign in.
 */
@Composable
fun SignInAction(message: UiText, onSignedIn: () -> Unit) {
    if (message !is UiText.SignInRequired && message !is UiText.OmniSignInRequired) return
    val app = LocalContext.current.applicationContext as TalosApp
    val config by app.configRepository.config.collectAsStateWithLifecycle()
    val omni = message is UiText.OmniSignInRequired
    val target = config?.let { if (omni) omniSignInContextFor(it) else signInContextFor(it) } ?: return
    var open by remember { mutableStateOf(false) }
    Button(onClick = { open = true }) { Text(stringResource(R.string.kube_signin_action)) }
    if (open) {
        val done = {
            open = false
            onSignedIn()
        }
        if (omni) {
            OmniSignInSheet(context = target, onDismiss = { open = false }, onSignedIn = done)
        } else {
            SignInSheet(context = target, onDismiss = { open = false }, onSignedIn = done)
        }
    }
}

/** The cluster on screen when it is reached through Omni: the one an Omni sign-in is for. */
private fun omniSignInContextFor(stored: StoredConfig): String? = stored.activeSummary?.takeIf { it.isOmni }?.name

/** How [info]'s sign-in stands: who, and until when when the core knows. */
@Composable
fun signInStatus(info: KubeSignInInfo): String = when {
    !info.signedIn -> stringResource(R.string.kube_signin_not_signed_in)
    info.user.isNotEmpty() && info.sessionExpires > 0 ->
        stringResource(R.string.kube_signin_signed_in_until, info.user, formatDateTime(info.sessionExpires * 1000))
    info.user.isNotEmpty() -> stringResource(R.string.kube_signin_signed_in_as, info.user)
    info.sessionExpires > 0 -> stringResource(R.string.kube_signin_signed_in_until_only, formatDateTime(info.sessionExpires * 1000))
    else -> stringResource(R.string.kube_signin_signed_in)
}

/**
 * A kubeconfig cluster's sign-in, from the cluster list: who is signed in and until when,
 * sign in (again), or sign out (its tokens and the secrets entered go).
 */
@Composable
fun SignInAccountDialog(cluster: ContextSummary, label: String, onSignIn: () -> Unit, onDismiss: () -> Unit) {
    val context = LocalContext.current
    val app = context.applicationContext as TalosApp
    val scope = rememberCoroutineScope()
    val info by produceState<Result<KubeSignInInfo?>?>(null, cluster.name) {
        value = runCatching { app.kubeAuthRepository.info(cluster.name) }
    }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.kube_signin_account_title, label)) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(stringResource(R.string.kube_signin_method, signInMethodText(cluster.signIn)))
                info?.fold(
                    onSuccess = { it?.let { i -> Text(signInStatus(i), style = MaterialTheme.typography.bodyMedium) } },
                    onFailure = { Text(it.userMessage(), color = LocalStatusColors.current.bad) },
                )
            }
        },
        confirmButton = {
            TextButton(onClick = onSignIn) {
                Text(stringResource(if (info?.getOrNull()?.signedIn == true) R.string.kube_signin_again else R.string.kube_signin_action))
            }
        },
        dismissButton = {
            Row {
                if (info?.getOrNull()?.signedIn == true) {
                    TextButton(onClick = {
                        scope.launch {
                            runCatching { app.kubeAuthRepository.signOut(cluster.name) }
                            app.signInChanged()
                            onDismiss()
                        }
                    }) { Text(stringResource(R.string.kube_signin_sign_out), color = LocalStatusColors.current.bad) }
                }
                TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_close)) }
            }
        },
    )
}

/**
 * A Talos cluster's Kubernetes access (K5): the admin kubeconfig Talos issues, or one of the
 * stored kubeconfig clusters ([kubeClusters]), whose credentials then reach Kubernetes.
 */
@Composable
fun KubeAccessDialog(
    label: String,
    current: String,
    kubeClusters: List<Pair<ContextSummary, String>>,
    onPick: (String?) -> Unit,
    onDismiss: () -> Unit,
) {
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.kube_access_title, label)) },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()).selectableGroup()) {
                MutedText(stringResource(R.string.kube_access_hint))
                AccessOption(stringResource(R.string.kube_access_talos), current.isEmpty()) { onPick(null) }
                kubeClusters.forEach { (cluster, name) ->
                    AccessOption(name, current == cluster.fingerprint) { onPick(cluster.fingerprint) }
                }
            }
        },
        confirmButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_close)) } },
    )
}

@Composable
private fun AccessOption(text: String, selected: Boolean, onClick: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().selectable(selected = selected, role = Role.RadioButton, onClick = onClick).padding(vertical = 4.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        RadioButton(selected = selected, onClick = null)
        Text(text, modifier = Modifier.padding(start = 8.dp))
    }
}
