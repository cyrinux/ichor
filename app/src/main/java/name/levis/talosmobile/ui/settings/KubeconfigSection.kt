package name.levis.talosmobile.ui.settings

import android.content.ActivityNotFoundException
import android.content.Context
import android.content.Intent
import android.net.Uri
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Card
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import name.levis.talosmobile.data.TalosRepository
import name.levis.talosmobile.model.ContextSummary
import name.levis.talosmobile.model.Feature
import name.levis.talosmobile.model.allows
import name.levis.talosmobile.ui.components.RoleNotice
import name.levis.talosmobile.security.AppLock
import name.levis.talosmobile.security.AuthResult
import name.levis.talosmobile.security.authenticate
import name.levis.talosmobile.security.findFragmentActivity
import name.levis.talosmobile.ui.components.SectionTitle
import name.levis.talosmobile.ui.theme.LocalStatusColors
import name.levis.talosmobile.ui.userMessage
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

private const val KUBENAV_PACKAGE = "io.kubenav.kubenav"

private sealed interface ExportState {
    data object Idle : ExportState
    data object Working : ExportState
    data object Saved : ExportState
    data class Failed(val message: String) : ExportState
}

/**
 * Exports the cluster's admin kubeconfig (os:admin role) to a file the user picks, then offers
 * to open kubenav, which imports kubeconfigs through its own file picker (it has no intents).
 */
@Composable
fun KubeconfigSection(talos: TalosRepository, appLock: AppLock, talosContext: ContextSummary) {
    val contextName = talosContext.name
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var state by remember { mutableStateOf<ExportState>(ExportState.Idle) }

    val saver = rememberLauncherForActivityResult(ActivityResultContracts.CreateDocument("application/yaml")) { uri ->
        if (uri == null) {
            state = ExportState.Idle
            return@rememberLauncherForActivityResult
        }
        state = ExportState.Working
        scope.launch {
            state = runCatching {
                val kubeconfig = talos.kubeconfig()
                withContext(Dispatchers.IO) { writeText(context, uri, kubeconfig) }
            }.fold(
                onSuccess = { ExportState.Saved },
                onFailure = { ExportState.Failed(it.userMessage()) },
            )
        }
    }

    fun export() {
        val activity = context.findFragmentActivity()
        if (!appLock.enabled.value || activity == null) {
            saver.launch("kubeconfig-$contextName.yaml")
            return
        }
        scope.launch {
            when (val auth = authenticate(activity, "Export kubeconfig")) {
                AuthResult.Success -> saver.launch("kubeconfig-$contextName.yaml")
                is AuthResult.Failure -> state = ExportState.Failed(auth.message)
            }
        }
    }

    SectionTitle("Kubernetes")
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(
                "Export an admin kubeconfig (needs an os:admin talosconfig). Anyone with this file has " +
                    "full access to the Kubernetes cluster: keep it private.",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            if (!talosContext.allows(Feature.KUBECONFIG)) {
                RoleNotice(Feature.KUBECONFIG, talosContext.roles)
            } else OutlinedButton(
                onClick = ::export,
                enabled = state != ExportState.Working,
                modifier = Modifier.fillMaxWidth(),
            ) { Text(if (state == ExportState.Working) "Exporting…" else "Export kubeconfig…") }
            when (val s = state) {
                ExportState.Saved -> Text("Saved. In kubenav, add a cluster from that file.", style = MaterialTheme.typography.bodySmall)
                is ExportState.Failed -> Text(s.message, color = LocalStatusColors.current.bad, style = MaterialTheme.typography.bodySmall)
                else -> Unit
            }
            OutlinedButton(onClick = { openKubenav(context) }, modifier = Modifier.fillMaxWidth()) { Text("Open kubenav") }
        }
    }
}

private fun writeText(context: Context, uri: Uri, text: String) {
    // "wt" truncates when the user picked an existing file.
    val stream = context.contentResolver.openOutputStream(uri, "wt") ?: error("Could not open the chosen file")
    stream.use { it.write(text.encodeToByteArray()) }
}

private fun openKubenav(context: Context) {
    val launch = context.packageManager.getLaunchIntentForPackage(KUBENAV_PACKAGE)
    val intent = launch ?: Intent(Intent.ACTION_VIEW, Uri.parse("market://details?id=$KUBENAV_PACKAGE"))
    try {
        context.startActivity(intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
    } catch (_: ActivityNotFoundException) {
        context.startActivity(
            Intent(Intent.ACTION_VIEW, Uri.parse("https://github.com/kubenav/kubenav")).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK),
        )
    }
}
