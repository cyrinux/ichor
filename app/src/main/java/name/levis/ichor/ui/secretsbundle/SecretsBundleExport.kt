package name.levis.ichor.ui.secretsbundle

import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.security.AuthResult
import name.levis.ichor.security.authenticate
import name.levis.ichor.security.findFragmentActivity
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.components.ConfirmDialog
import name.levis.ichor.ui.components.writeText
import name.levis.ichor.ui.uiText

/** The file name `talosctl gen config --with-secrets` is used with. */
const val SECRETS_BUNDLE_FILE = "secrets.yaml"

/**
 * Export of the cluster's secrets bundle: a warning, the app lock, then the system "Save to…"
 * picker; the bundle is fetched only once a document is picked and written straight into it,
 * so it never sits in app storage or in screen state. Returns the action that starts it.
 * [fetch] reads the bundle from a control plane; [onResult] gets the message to show.
 */
@Composable
fun rememberSecretsBundleExport(
    clusterName: String,
    fetch: suspend () -> String,
    onResult: (UiText) -> Unit,
): () -> Unit {
    val context = LocalContext.current
    val app = context.applicationContext as TalosApp
    val scope = rememberCoroutineScope()
    val currentFetch by rememberUpdatedState(fetch)
    val currentResult by rememberUpdatedState(onResult)
    var confirming by rememberSaveable { mutableStateOf(false) }
    val authTitle = stringResource(R.string.secrets_bundle_auth, clusterName)

    val saver = rememberLauncherForActivityResult(ActivityResultContracts.CreateDocument("application/yaml")) { uri ->
        if (uri == null) return@rememberLauncherForActivityResult
        scope.launch {
            val result = try {
                val bundle = currentFetch()
                withContext(Dispatchers.IO) { writeText(context, uri, bundle, R.string.secrets_bundle_open_failed) }
                UiText.Res(R.string.secrets_bundle_saved)
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                // The picked document stays empty: nothing of the bundle was written.
                runCatching { context.contentResolver.delete(uri, null, null) }
                UiText.Res(R.string.secrets_bundle_failed, e.uiText())
            }
            currentResult(result)
        }
    }

    fun confirmed() {
        confirming = false
        val activity = context.findFragmentActivity()
        if (!app.appLock.enabled.value || activity == null) {
            saver.launch(SECRETS_BUNDLE_FILE)
            return
        }
        scope.launch {
            when (val auth = authenticate(activity, authTitle)) {
                AuthResult.Success -> saver.launch(SECRETS_BUNDLE_FILE)
                is AuthResult.Failure -> currentResult(UiText.Raw(auth.message))
            }
        }
    }

    if (confirming) {
        ConfirmDialog(
            title = stringResource(R.string.secrets_bundle_confirm_title),
            text = stringResource(R.string.secrets_bundle_confirm_body, clusterName),
            confirm = stringResource(R.string.secrets_bundle_confirm),
            onConfirm = ::confirmed,
            onDismiss = { confirming = false },
            destructive = true,
        )
    }

    return { confirming = true }
}
