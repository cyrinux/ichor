package name.levis.ichor.ui.kubeauth

import android.widget.Toast
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
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
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.model.GCP_OAUTH_CLIENT_ID
import name.levis.ichor.model.GCP_USER_CREDENTIALS
import name.levis.ichor.model.KubeSignInInfo
import name.levis.ichor.model.SignInPrompt
import name.levis.ichor.model.credentialsComplete
import name.levis.ichor.model.fieldSetLabel
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.copyToClipboard
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * Signs the kubeconfig cluster [context] in, in a sheet: the credentials form, or the browser
 * or device-code sign-in. [onSignedIn] follows a sign-in that worked; the sheet then closes.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SignInSheet(context: String, onDismiss: () -> Unit, onSignedIn: () -> Unit) {
    val app = LocalContext.current.applicationContext as TalosApp
    val vm: SignInViewModel = viewModel(key = "kube-sign-in-$context", factory = factory { SignInViewModel(app.kubeAuthRepository, context, onBrowserDone = app::bringToFront) })
    val state by vm.state.collectAsStateWithLifecycle()

    LaunchedEffect(context) { vm.load() }
    // Closing the sheet stops waiting for the browser; the next one starts afresh.
    DisposableEffect(vm) { onDispose { vm.close() } }
    LaunchedEffect(state) {
        if (state is SignInUi.Done) {
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
                SignInUi.Loading, SignInUi.Done -> Box(Modifier.fillMaxWidth().padding(24.dp), Alignment.Center) { CircularProgressIndicator() }
                is SignInUi.Failed -> {
                    Text(s.message, color = LocalStatusColors.current.bad)
                    TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_close)) }
                }
                is SignInUi.Ready -> {
                    SignInTitle(s.info)
                    if (s.info.kind == KubeSignInInfo.KIND_CREDENTIALS) {
                        CredentialsForm(s.info, s.checking, onSubmit = vm::submit)
                    } else {
                        MutedText(stringResource(R.string.kube_signin_browser_hint))
                        Button(onClick = vm::startSignIn, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.kube_signin_action)) }
                    }
                    s.error?.let { Text(it, color = LocalStatusColors.current.bad, style = MaterialTheme.typography.bodyMedium) }
                }
                is SignInUi.Waiting -> {
                    SignInTitle(s.info)
                    Waiting(s.prompt, onCancel = vm::cancel)
                }
            }
        }
    }
}

@Composable
private fun SignInTitle(info: KubeSignInInfo) {
    Text(stringResource(R.string.kube_signin_title, signInMethodText(info.method)), style = MaterialTheme.typography.titleMedium)
}

/** The fields to enter; EKS first asks which kind of credentials. */
@Composable
private fun CredentialsForm(info: KubeSignInInfo, checking: Boolean, onSubmit: (List<String>, Map<String, String>) -> Unit) {
    // Not saveable: secrets never go into saved instance state. Starts with what the last
    // sign-in entered (never a secret), so renewing a session is one tap.
    val values = remember(info.method) { mutableStateMapOf<String, String>().apply { putAll(info.values) } }
    var option by remember(info.method) { mutableIntStateOf(info.rememberedOption) }
    val sets = info.fieldSets
    val fields = sets.getOrElse(option) { sets.first() }
    if (sets.size > 1) {
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            sets.forEachIndexed { index, set ->
                FilterChip(selected = option == index, onClick = { option = index }, label = { Text(stringResource(fieldSetLabel(set))) }, enabled = !checking)
            }
        }
    }
    MutedText(stringResource(R.string.kube_signin_credentials_hint))
    CredentialFields(fields, values, onValue = { name, value -> values[name] = value }, enabled = !checking)
    if (GCP_USER_CREDENTIALS in fields) MutedText(stringResource(R.string.kube_signin_gcp_user_hint))
    if (GCP_OAUTH_CLIENT_ID in fields) MutedText(stringResource(R.string.kube_signin_gcp_oauth_hint))
    Button(
        onClick = { onSubmit(fields, values.toMap()) },
        enabled = !checking && credentialsComplete(fields, values),
        modifier = Modifier.fillMaxWidth(),
    ) {
        if (checking) {
            CircularProgressIndicator(Modifier.size(18.dp), strokeWidth = 2.dp)
        } else {
            Text(stringResource(R.string.kube_signin_action))
        }
    }
}

/**
 * A sign-in in progress: the browser opens by itself and comes back on its own; a device
 * code is shown big, to copy, with a button opening the page to enter it.
 */
@Composable
private fun Waiting(prompt: SignInPrompt?, onCancel: () -> Unit) {
    val context = LocalContext.current
    val noBrowser = stringResource(R.string.kube_signin_no_browser)
    val open = { url: String -> if (!openInBrowser(context, url)) Toast.makeText(context, noBrowser, Toast.LENGTH_LONG).show() }
    // Once per page: the prompt comes again only for a new one.
    LaunchedEffect(prompt?.url) {
        if (prompt != null && !prompt.isDevice && prompt.url.isNotEmpty()) open(prompt.url)
    }
    when {
        prompt == null -> Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp)
            Text(stringResource(R.string.kube_signin_starting))
        }
        prompt.isDevice -> DeviceCode(prompt, onOpen = { open(prompt.url.ifEmpty { prompt.verificationUrl }) })
        else -> {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp)
                Text(stringResource(R.string.kube_signin_waiting_browser))
            }
            OutlinedButton(onClick = { open(prompt.url) }) { Text(stringResource(R.string.kube_signin_open_again)) }
        }
    }
    TextButton(onClick = onCancel) { Text(stringResource(R.string.common_cancel)) }
}

@Composable
private fun DeviceCode(prompt: SignInPrompt, onOpen: () -> Unit) {
    val context = LocalContext.current
    Text(stringResource(R.string.kube_signin_device_hint, prompt.verificationUrl.ifEmpty { prompt.url }), style = MaterialTheme.typography.bodyMedium)
    SelectionContainer {
        Text(
            prompt.userCode,
            fontFamily = FontFamily.Monospace,
            fontWeight = FontWeight.Bold,
            fontSize = 32.sp,
            modifier = Modifier.fillMaxWidth().padding(vertical = 8.dp),
        )
    }
    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        OutlinedButton(onClick = { copyToClipboard(context, context.getString(R.string.kube_signin_code), prompt.userCode) }) {
            Icon(Icons.Outlined.ContentCopy, contentDescription = null, modifier = Modifier.padding(end = 8.dp).size(18.dp))
            Text(stringResource(R.string.kube_signin_copy_code))
        }
        Button(onClick = onOpen) { Text(stringResource(R.string.kube_signin_open_page)) }
    }
    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
        CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp)
        MutedText(stringResource(R.string.kube_signin_waiting_device))
    }
}

/** The method's short name ("AWS EKS"), or the core's name for one the app does not know. */
@Composable
internal fun signInMethodText(method: String): String =
    name.levis.ichor.model.signInMethodName(method)?.let { stringResource(it) } ?: method
