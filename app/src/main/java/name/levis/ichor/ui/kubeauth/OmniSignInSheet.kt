package name.levis.ichor.ui.kubeauth

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.model.KubeSignInInfo
import name.levis.ichor.model.OmniSignInInfo
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * Signs the Omni cluster [context] in, in a sheet: a service account key to paste, or the
 * browser. Also signs it out. [onSignedIn] follows a change that worked; the sheet then closes.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun OmniSignInSheet(context: String, onDismiss: () -> Unit, onSignedIn: () -> Unit) {
    val app = LocalContext.current.applicationContext as TalosApp
    val vm: OmniSignInViewModel = viewModel(
        key = "omni-sign-in-$context",
        factory = factory { OmniSignInViewModel(app.omniAuthRepository, context, onBrowserDone = app::bringToFront) },
    )
    val state by vm.state.collectAsStateWithLifecycle()

    LaunchedEffect(context) { vm.load() }
    DisposableEffect(vm) { onDispose { vm.close() } }
    LaunchedEffect(state) {
        if (state is OmniSignInUi.Done) {
            app.signInChanged()
            onSignedIn()
        }
    }

    ModalBottomSheet(onDismissRequest = onDismiss, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)) {
        Column(
            Modifier.verticalScroll(rememberScrollState()).padding(horizontal = 24.dp).padding(bottom = 24.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            when (val s = state) {
                OmniSignInUi.Loading, OmniSignInUi.Done -> Box(Modifier.fillMaxWidth().padding(24.dp), Alignment.Center) { CircularProgressIndicator() }
                is OmniSignInUi.Failed -> {
                    Text(s.message, color = LocalStatusColors.current.bad)
                    TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_close)) }
                }
                is OmniSignInUi.Ready -> {
                    OmniTitle(s.info)
                    OmniMethods(s, onKey = vm::submitKey, onBrowser = vm::startBrowser, onSignOut = vm::signOut)
                }
                is OmniSignInUi.Waiting -> {
                    OmniTitle(s.info)
                    Waiting(s.prompt, onCancel = vm::cancel)
                }
            }
        }
    }
}

@Composable
private fun OmniTitle(info: OmniSignInInfo) {
    Text(stringResource(R.string.omni_signin_title), style = MaterialTheme.typography.titleMedium)
    if (info.instance.isNotEmpty()) MutedText(info.instance)
    Text(signInStatus(KubeSignInInfo(signedIn = info.signedIn, user = info.user, sessionExpires = info.sessionExpires)), style = MaterialTheme.typography.bodyMedium)
}

/** The service account key, then the browser (with the context's email), then signing out. */
@Composable
private fun OmniMethods(ready: OmniSignInUi.Ready, onKey: (String) -> Unit, onBrowser: () -> Unit, onSignOut: () -> Unit) {
    // Not saveable: the key never goes into saved instance state.
    var key by remember { mutableStateOf("") }
    MutedText(stringResource(R.string.omni_signin_key_hint))
    OmniKeyField(key, { key = it }, enabled = !ready.checking)
    Button(onClick = { onKey(key) }, enabled = !ready.checking && key.isNotBlank(), modifier = Modifier.fillMaxWidth()) {
        if (ready.checking) {
            CircularProgressIndicator(Modifier.size(18.dp), strokeWidth = 2.dp)
        } else {
            Text(stringResource(R.string.omni_signin_use_key))
        }
    }
    HorizontalDivider()
    if (ready.info.identity.isNotEmpty()) {
        MutedText(stringResource(R.string.omni_signin_browser_hint, ready.info.identity))
        OutlinedButton(onClick = onBrowser, enabled = !ready.checking, modifier = Modifier.fillMaxWidth()) {
            Text(stringResource(R.string.omni_signin_browser))
        }
    } else {
        MutedText(stringResource(R.string.omni_signin_needs_identity))
    }
    ready.error?.let { Text(it, color = LocalStatusColors.current.bad, style = MaterialTheme.typography.bodyMedium) }
    if (ready.info.signedIn) {
        TextButton(onClick = onSignOut, enabled = !ready.checking) {
            Text(stringResource(R.string.kube_signin_sign_out), color = LocalStatusColors.current.bad)
        }
    }
}

/** An Omni service account key (base64, as Omni shows it), masked like other secrets. */
@Composable
fun OmniKeyField(value: String, onValue: (String) -> Unit, enabled: Boolean = true, optional: Boolean = false) {
    val label = stringResource(R.string.omni_field_service_account_key)
    SecretField(value, onValue, if (optional) stringResource(R.string.kube_field_optional, label) else label, enabled)
}
