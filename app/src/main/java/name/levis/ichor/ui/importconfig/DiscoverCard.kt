package name.levis.ichor.ui.importconfig

import androidx.annotation.StringRes
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.FilterChip
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.GoogleAuthorization
import name.levis.ichor.data.googleSignInSecrets
import name.levis.ichor.model.DiscoveryProgress
import name.levis.ichor.model.DiscoveryProvider
import name.levis.ichor.model.GCP_GOOGLE_SIGN_IN
import name.levis.ichor.model.GCP_OAUTH_CLIENT_ID
import name.levis.ichor.model.GCP_PROJECTS
import name.levis.ichor.model.GCP_USER_CREDENTIALS
import name.levis.ichor.model.credentialsComplete
import name.levis.ichor.model.credentialsFor
import name.levis.ichor.model.discoveryNeedsSignIn
import name.levis.ichor.model.discoveryUsesGoogle
import name.levis.ichor.model.fieldSetLabel
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.kubeauth.CredentialFields
import name.levis.ichor.ui.kubeauth.Waiting
import name.levis.ichor.ui.kubeauth.rememberGoogleSignIn
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * Adding clusters from a cloud account (K7): pick the provider, enter the account's
 * credentials, and its clusters show in the kubeconfig preview. Nothing is stored before
 * the user adds them there; the clusters then sign in with the same credentials. Google GKE
 * also takes Google's own sign-in (Play build) and the organisation's OAuth client, which
 * signs in in the browser first ([onDiscoverWithSignIn]).
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun DiscoverCard(
    state: ImportState.Discover,
    onDiscover: (DiscoveryProvider, Map<String, String>) -> Unit,
    onDiscoverWithSignIn: (DiscoveryProvider, Map<String, String>) -> Unit,
    onCancelSignIn: () -> Unit,
    onSignInCode: (String) -> Unit,
    onFailed: (String) -> Unit,
    onCancel: () -> Unit,
) {
    val providers = state.options.keys.toList()
    var provider by remember { mutableStateOf(state.initial?.takeIf { it in providers } ?: providers.firstOrNull()) }
    // Not saveable: secrets never go into saved instance state.
    val values = remember { mutableStateMapOf<String, String>() }
    val sets = provider?.let { state.options[it] }.orEmpty()
    var option by remember(provider) { mutableIntStateOf(0) }
    val fields = sets.getOrElse(option) { sets.firstOrNull().orEmpty() }
    Column(
        Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text(stringResource(R.string.kube_discover_title), style = MaterialTheme.typography.titleLarge)
        MutedText(stringResource(R.string.kube_discover_body))
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            providers.forEach { p ->
                FilterChip(
                    selected = provider == p,
                    onClick = { provider = p },
                    label = { Text(stringResource(p.label)) },
                    enabled = !state.running,
                )
            }
        }
        if (sets.size > 1) {
            FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                sets.forEachIndexed { index, set ->
                    FilterChip(
                        selected = option == index,
                        onClick = {
                            option = index
                            values.clear()
                        },
                        label = { Text(stringResource(fieldSetLabel(set))) },
                        enabled = !state.running,
                    )
                }
            }
        }
        // Google's own sign-in: nothing to type but the optional projects.
        val typed = fields.filter { it != GCP_GOOGLE_SIGN_IN }
        CredentialFields(typed, values, onValue = { name, value -> values[name] = value }, enabled = !state.running)
        DiscoverHints(fields)
        if (state.running && !state.signingIn) DiscoverProgressRow(state.progress)
        if (state.needsSignIn) Text(stringResource(R.string.kube_discover_signin_first), style = MaterialTheme.typography.bodyMedium)
        state.error?.let { Text(it, color = LocalStatusColors.current.bad, style = MaterialTheme.typography.bodyMedium) }
        if (state.signingIn) {
            Waiting(state.prompt, onCancel = onCancelSignIn, onCode = onSignInCode)
        } else {
            val discover = { p: DiscoveryProvider -> if (discoveryNeedsSignIn(fields)) onDiscoverWithSignIn(p, credentialsFor(fields, values)) else onDiscover(p, credentialsFor(fields, values)) }
            DiscoverActions(
                running = state.running,
                google = provider != null && discoveryUsesGoogle(fields),
                label = if (discoveryNeedsSignIn(fields)) R.string.kube_discover_signin_find else R.string.kube_discover_find,
                enabled = provider != null && credentialsComplete(fields, values),
                onGoogle = { token -> provider?.let { onDiscover(it, googleSignInSecrets(token) + (GCP_PROJECTS to values[GCP_PROJECTS].orEmpty().trim())) } },
                onDiscover = { provider?.let(discover) },
                onFailed = onFailed,
                onCancel = onCancel,
            )
        }
        Box(Modifier.height(8.dp))
    }
}

/** What each GKE credential needs explained, and the least-privilege reminder. */
@Composable
private fun DiscoverHints(fields: List<String>) {
    if (discoveryUsesGoogle(fields)) {
        MutedText(stringResource(R.string.kube_discover_google_body))
        MutedText(stringResource(R.string.kube_signin_google_hint))
    }
    if (GCP_USER_CREDENTIALS in fields) {
        MutedText(stringResource(R.string.kube_signin_gcp_user_hint))
        MutedText(stringResource(R.string.kube_signin_gcp_workforce_hint))
    }
    if (GCP_OAUTH_CLIENT_ID in fields) {
        MutedText(stringResource(R.string.kube_signin_gcp_oauth_hint))
        MutedText(stringResource(R.string.kube_signin_gcp_oauth_web_hint))
    }
    if (GCP_PROJECTS in fields) MutedText(stringResource(R.string.kube_discover_gcp_projects_hint))
    MutedText(stringResource(R.string.kube_discover_least_privilege))
}

/**
 * Cancel and the card's action: "Sign in with Google" ([google], Google's picker, then
 * [onGoogle] with the token), else [label] ("Find clusters", or "Sign in and find clusters"
 * for the OAuth client).
 */
@Composable
private fun DiscoverActions(
    running: Boolean,
    google: Boolean,
    @StringRes label: Int,
    enabled: Boolean,
    onGoogle: (GoogleAuthorization.Token) -> Unit,
    onDiscover: () -> Unit,
    onFailed: (String) -> Unit,
    onCancel: () -> Unit,
) {
    val app = LocalContext.current.applicationContext as TalosApp
    val signInWithGoogle = rememberGoogleSignIn(app.googleNativeSignIn) { answer, failedText ->
        when (answer) {
            is GoogleAuthorization.Token -> onGoogle(answer)
            is GoogleAuthorization.Failed -> onFailed(answer.message.ifBlank { failedText })
            is GoogleAuthorization.NeedsUser -> Unit
        }
    }
    Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
        OutlinedButton(onClick = onCancel, enabled = !running, modifier = Modifier.weight(1f)) { Text(stringResource(R.string.common_cancel)) }
        Button(
            onClick = if (google) signInWithGoogle else onDiscover,
            enabled = !running && (google || enabled),
            modifier = Modifier.weight(1f),
        ) {
            if (running) {
                CircularProgressIndicator(Modifier.size(18.dp), strokeWidth = 2.dp)
            } else {
                Text(stringResource(if (google) R.string.kube_signin_option_google else label))
            }
        }
    }
}

/**
 * How far the running discovery got: a bar that fills as a Google account's projects are
 * read (indeterminate while they are listed, and for the other clouds), and the counts.
 */
@Composable
private fun DiscoverProgressRow(progress: DiscoveryProgress?) {
    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
        val projects = progress?.projects ?: 0
        if (projects > 0) {
            LinearProgressIndicator(
                progress = { (progress?.scanned ?: 0).toFloat() / projects },
                modifier = Modifier.fillMaxWidth(),
            )
            MutedText(
                stringResource(
                    R.string.kube_discover_progress_projects,
                    (progress?.scanned ?: 0).toString(),
                    projects.toString(),
                    (progress?.clusters ?: 0).toString(),
                ),
            )
        } else {
            LinearProgressIndicator(Modifier.fillMaxWidth())
            MutedText(stringResource(R.string.kube_discover_progress_looking))
        }
    }
}
