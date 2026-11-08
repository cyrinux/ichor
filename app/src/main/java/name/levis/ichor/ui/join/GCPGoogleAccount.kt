package name.levis.ichor.ui.join

import androidx.annotation.StringRes
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.data.SignInEvent
import name.levis.ichor.model.GCPCheck
import name.levis.ichor.model.GCPSignInKind
import name.levis.ichor.model.GCPSignInResult
import name.levis.ichor.model.GCP_LOGIN_CONFIG
import name.levis.ichor.model.GCP_OAUTH_CLIENT_ID
import name.levis.ichor.model.SignInPrompt
import name.levis.ichor.model.gcpCredentialWith
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.design.IchorButton
import name.levis.ichor.ui.design.IchorFilterChip
import name.levis.ichor.ui.design.IchorOutlinedButton
import name.levis.ichor.ui.kubeauth.Waiting
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.uiStateOf

/** The OAuth client's other fields, as the GKE sign-in reads them. */
private const val GCP_OAUTH_CLIENT_SECRET = "gcpOAuthClientSecret"
private const val GCP_OAUTH_REDIRECT_URL = "gcpOAuthRedirectUrl"

/** Where the Google-account side of the account dialog is. */
private sealed interface GoogleStep {
    data object Form : GoogleStep
    data object Working : GoogleStep
    data class Browser(val prompt: SignInPrompt?) : GoogleStep
    data class Projects(val result: GCPSignInResult) : GoogleStep
    data class Failed(val message: UiText) : GoogleStep
    data class Chosen(val result: GCPSignInResult, val project: String, val check: UiState<GCPCheck>) : GoogleStep
}

/** The label and hint of a sign-in kind. */
@StringRes
private fun kindLabel(kind: String): Int = when (kind) {
    GCPSignInKind.OAUTH -> R.string.kube_signin_option_gcp_oauth
    GCPSignInKind.WORKFORCE -> R.string.kube_signin_option_gcp_workforce
    else -> R.string.kube_signin_option_gcp_gcloud
}

@StringRes
private fun kindHint(kind: String): Int = when (kind) {
    GCPSignInKind.OAUTH -> R.string.kube_signin_gcp_oauth_hint
    GCPSignInKind.WORKFORCE -> R.string.kube_signin_gcp_workforce_config_hint
    else -> R.string.kube_signin_gcp_gcloud_hint
}

/**
 * The Google-account side of the account dialog: the sign-in kind (gcloud's client, an OAuth
 * client of your own, a workforce login config), the browser sign-in, the project the calls are
 * billed to, then the same test as a key. [onReady] gets the credential JSON to seal and its test
 * once they pass, null meanwhile. The sign-in state stays in that JSON, never on screen.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun GoogleAccountSide(vm: GCPCreateViewModel, onReady: (credential: String?, check: GCPCheck?) -> Unit) {
    val colors = LocalStatusColors.current
    val scope = rememberCoroutineScope()
    var kinds by remember { mutableStateOf(GCPSignInKind.browser) }
    var kind by remember { mutableStateOf(GCPSignInKind.GCLOUD) }
    var clientId by remember { mutableStateOf("") }
    var clientSecret by remember { mutableStateOf("") }
    var redirect by remember { mutableStateOf("") }
    var loginConfig by remember { mutableStateOf("") }
    var step by remember { mutableStateOf<GoogleStep>(GoogleStep.Form) }
    var signInJob by remember { mutableStateOf<Job?>(null) }
    LaunchedEffect(Unit) {
        // Android offers the browser sign-ins only; "native" is the iOS build's.
        val offered = runCatching { vm.signInChoices() }.getOrNull()?.filter { it in GCPSignInKind.browser }
        if (!offered.isNullOrEmpty()) {
            kinds = offered
            if (kind !in offered) kind = offered.first()
        }
    }
    // The sheet closed or the dialog switched to a key: the wait for the browser ends.
    DisposableEffect(Unit) { onDispose { signInJob?.cancel() } }

    fun choose(result: GCPSignInResult, project: String) {
        val credential = gcpCredentialWith(result.credential, "project" to project.trim()) ?: return
        step = GoogleStep.Working
        scope.launch {
            val check = uiStateOf { vm.test(credential) }
            val tested = (check as? UiState.Loaded)?.data
            if (tested != null) onReady(tested.credential.ifBlank { credential }, tested)
            step = GoogleStep.Chosen(result, project.trim(), check)
        }
    }

    fun secrets(): Map<String, String> = when (kind) {
        GCPSignInKind.OAUTH -> mapOf(
            GCP_OAUTH_CLIENT_ID to clientId.trim(),
            GCP_OAUTH_CLIENT_SECRET to clientSecret.trim(),
            GCP_OAUTH_REDIRECT_URL to redirect.trim(),
        )
        GCPSignInKind.WORKFORCE -> mapOf(GCP_LOGIN_CONFIG to loginConfig.trim())
        else -> emptyMap()
    }

    fun signIn() {
        onReady(null, null)
        step = GoogleStep.Browser(null)
        signInJob = scope.launch {
            var failure: String? = null
            vm.signInStart(kind, secrets()).collect { event ->
                when (event) {
                    is SignInEvent.Prompt -> step = GoogleStep.Browser(event.prompt)
                    is SignInEvent.Done -> failure = event.error
                }
            }
            // Cancelled meanwhile: the form is back already.
            if (step !is GoogleStep.Browser) return@launch
            failure?.let {
                step = GoogleStep.Failed(UiText.Raw(it))
                return@launch
            }
            step = GoogleStep.Working
            when (val result = uiStateOf { vm.signInCredential() }) {
                is UiState.Loaded -> {
                    // The only project the account sees is picked by itself.
                    val only = result.data.projects.singleOrNull()?.takeIf { !result.data.more }
                    if (only != null) choose(result.data, only.id) else step = GoogleStep.Projects(result.data)
                }
                is UiState.Failed -> step = GoogleStep.Failed(result.message)
                else -> step = GoogleStep.Form
            }
        }
    }

    Text(stringResource(R.string.gcp_signin_warning), color = colors.warn, style = MaterialTheme.typography.bodySmall)
    when (val current = step) {
        GoogleStep.Form -> {
            FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                kinds.forEach { k ->
                    IchorFilterChip(selected = k == kind, onClick = { kind = k }, label = { Text(stringResource(kindLabel(k))) })
                }
            }
            MutedText(stringResource(kindHint(kind)))
            when (kind) {
                GCPSignInKind.OAUTH -> {
                    IdField(clientId, R.string.kube_field_gcp_oauth_client_id, secret = false) { clientId = it }
                    IdField(clientSecret, R.string.kube_field_gcp_oauth_client_secret, secret = true) { clientSecret = it }
                    IdField(redirect, R.string.kube_field_gcp_oauth_redirect, secret = false) { redirect = it }
                    MutedText(stringResource(R.string.kube_signin_gcp_oauth_web_hint))
                }
                GCPSignInKind.WORKFORCE -> OutlinedTextField(
                    value = loginConfig,
                    onValueChange = { loginConfig = it },
                    label = { Text(stringResource(R.string.kube_field_gcp_login_config)) },
                    maxLines = 4,
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Ascii, autoCorrectEnabled = false),
                    modifier = Modifier.fillMaxWidth(),
                )
                else -> Unit
            }
            val ready = when (kind) {
                GCPSignInKind.OAUTH -> clientId.isNotBlank()
                GCPSignInKind.WORKFORCE -> loginConfig.isNotBlank()
                else -> true
            }
            IchorButton(onClick = ::signIn, enabled = ready, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.kube_signin_action)) }
        }
        GoogleStep.Working -> LoadingBox()
        is GoogleStep.Browser -> Waiting(current.prompt, onCancel = {
            signInJob?.cancel()
            step = GoogleStep.Form
        }, onCode = vm::signInComplete)
        is GoogleStep.Projects -> ProjectPick(current.result) { choose(current.result, it) }
        is GoogleStep.Failed -> ErrorBox(current.message, onRetry = { step = GoogleStep.Form })
        is GoogleStep.Chosen -> {
            Text(stringResource(R.string.gcp_signin_signed_in, current.result.account), style = MaterialTheme.typography.bodySmall)
            when (val check = current.check) {
                is UiState.Loaded -> {
                    GCPCheckResult(check.data)
                    if (check.data.renewHint.isNotBlank()) MutedText(check.data.renewHint)
                }
                is UiState.Failed -> {
                    ErrorBox(check.message, onRetry = { choose(current.result, current.project) })
                    GCPActivationLink(check.message)
                }
                else -> Unit
            }
            // Another project of the same sign-in, without signing in again.
            IchorOutlinedButton(onClick = {
                onReady(null, null)
                step = GoogleStep.Projects(current.result)
            }) { Text(stringResource(R.string.gcp_signin_other_project)) }
        }
    }
}

/** The project the calls are billed to: the listed ones, or the ID of one the list leaves out. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun ProjectPick(result: GCPSignInResult, onPick: (String) -> Unit) {
    var typed by remember { mutableStateOf("") }
    Text(stringResource(R.string.gcp_signin_signed_in, result.account), style = MaterialTheme.typography.bodySmall)
    Text(stringResource(R.string.gcp_signin_project), style = MaterialTheme.typography.labelLarge)
    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        result.projects.forEach { p ->
            val label = if (p.name.isBlank() || p.name == p.id) p.id else "${p.name} (${p.id})"
            IchorFilterChip(selected = false, onClick = { onPick(p.id) }, label = { Text(label) })
        }
    }
    if (result.more || result.projects.isEmpty()) {
        MutedText(stringResource(R.string.gcp_signin_more))
        IdField(typed, R.string.gcp_signin_project_id, secret = false) { typed = it }
        IchorOutlinedButton(onClick = { onPick(typed) }, enabled = typed.isNotBlank()) { Text(stringResource(R.string.gcp_signin_use_project)) }
    }
}
