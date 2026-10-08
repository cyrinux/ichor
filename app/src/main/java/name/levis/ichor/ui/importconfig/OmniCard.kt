package name.levis.ichor.ui.importconfig

import android.widget.Toast
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.FilterChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.credentialsComplete
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.kubeauth.CredentialFields
import name.levis.ichor.ui.kubeauth.openInBrowser
import name.levis.ichor.ui.theme.LocalStatusColors

private const val FIELD_URL = "omniUrl"
private const val FIELD_EMAIL = "omniEmail"
private const val FIELD_KEY = "serviceAccountKey"

/**
 * Adding the clusters of a Sidero Omni account: the instance's URL, then an account (it
 * confirms a key in the browser) or a service account key. Its clusters show in the
 * talosconfig preview, already signed in.
 */
@Composable
internal fun OmniCard(
    state: ImportState.Omni,
    onAccount: (endpoint: String, email: String) -> Unit,
    onServiceAccount: (endpoint: String, key: String) -> Unit,
    onCancel: () -> Unit,
) {
    val context = LocalContext.current
    var serviceAccount by remember { mutableStateOf(false) }
    // Not saveable: a service account key never goes into saved instance state.
    val values = remember { mutableStateMapOf<String, String>() }
    val fields = listOf(FIELD_URL, if (serviceAccount) FIELD_KEY else FIELD_EMAIL)
    val noBrowser = stringResource(R.string.kube_signin_no_browser)
    // Once per page: the prompt comes again only for a new one.
    LaunchedEffect(state.prompt?.url) {
        state.prompt?.url?.takeIf { it.isNotEmpty() }?.let { url ->
            if (!openInBrowser(context, url)) Toast.makeText(context, noBrowser, Toast.LENGTH_LONG).show()
        }
    }
    Column(
        Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text(stringResource(R.string.omni_add_title), style = MaterialTheme.typography.titleLarge)
        MutedText(stringResource(R.string.omni_add_body))
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            FilterChip(
                selected = !serviceAccount,
                onClick = { serviceAccount = false },
                label = { Text(stringResource(R.string.omni_mode_account)) },
                enabled = !state.running,
            )
            FilterChip(
                selected = serviceAccount,
                onClick = { serviceAccount = true },
                label = { Text(stringResource(R.string.kube_signin_method_omni_sa)) },
                enabled = !state.running,
            )
        }
        CredentialFields(fields, values, onValue = { name, value -> values[name] = value }, enabled = !state.running)
        state.prompt?.let { prompt ->
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp)
                Text(stringResource(R.string.kube_signin_waiting_browser))
            }
            OutlinedButton(onClick = { openInBrowser(context, prompt.url) }) { Text(stringResource(R.string.kube_signin_open_again)) }
        }
        state.error?.let { Text(it, color = LocalStatusColors.current.bad, style = MaterialTheme.typography.bodyMedium) }
        Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            OutlinedButton(onClick = onCancel, modifier = Modifier.weight(1f)) { Text(stringResource(R.string.common_cancel)) }
            Button(
                onClick = {
                    val url = values[FIELD_URL].orEmpty().trim()
                    if (serviceAccount) onServiceAccount(url, values[FIELD_KEY].orEmpty().trim()) else onAccount(url, values[FIELD_EMAIL].orEmpty().trim())
                },
                enabled = !state.running && credentialsComplete(fields, values),
                modifier = Modifier.weight(1f),
            ) {
                if (state.running) {
                    CircularProgressIndicator(Modifier.size(18.dp), strokeWidth = 2.dp)
                } else {
                    Text(stringResource(R.string.kube_signin_action))
                }
            }
        }
        Box(Modifier.height(8.dp))
    }
}
