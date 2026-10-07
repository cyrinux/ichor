package name.levis.ichor.ui.settings

import name.levis.ichor.R
import androidx.compose.ui.res.stringResource
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
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.Feature
import name.levis.ichor.model.allows
import name.levis.ichor.model.isKube
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.RoleNotice
import name.levis.ichor.security.AppLock
import name.levis.ichor.security.AuthResult
import name.levis.ichor.security.authenticate
import name.levis.ichor.security.findFragmentActivity
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.writeText
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.uiText
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

private const val KUBENAV_PACKAGE = "io.kubenav.kubenav"

private sealed interface ExportState {
    data object Idle : ExportState
    data object Working : ExportState
    data object Saved : ExportState
    data class Failed(val message: UiText) : ExportState
}

/**
 * Exports the cluster's admin kubeconfig (os:admin role), or the stored one of a cluster added
 * from a kubeconfig, to a file the user picks, then offers to open kubenav, which imports
 * kubeconfigs through its own file picker (it has no intents).
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
                withContext(Dispatchers.IO) { writeText(context, uri, kubeconfig, R.string.settings_kube_open_failed) }
            }.fold(
                onSuccess = { ExportState.Saved },
                onFailure = { ExportState.Failed(it.uiText()) },
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
            when (val auth = authenticate(activity, context.getString(R.string.settings_kube_auth))) {
                AuthResult.Success -> saver.launch("kubeconfig-$contextName.yaml")
                is AuthResult.Failure -> state = ExportState.Failed(UiText.Raw(auth.message))
            }
        }
    }

    SectionTitle(stringResource(R.string.settings_kube_section))
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            // A cluster added from a kubeconfig exports that kubeconfig, not one Talos issues.
            MutedText(stringResource(if (talosContext.isKube) R.string.settings_kube_desc_imported else R.string.settings_kube_desc))
            if (!talosContext.allows(Feature.KUBECONFIG)) {
                RoleNotice(Feature.KUBECONFIG, talosContext.roles)
            } else OutlinedButton(
                onClick = ::export,
                enabled = state != ExportState.Working,
                modifier = Modifier.fillMaxWidth(),
            ) { Text(stringResource(if (state == ExportState.Working) R.string.settings_kube_exporting else R.string.settings_kube_export)) }
            when (val s = state) {
                ExportState.Saved -> Text(stringResource(R.string.settings_kube_saved), style = MaterialTheme.typography.bodySmall)
                is ExportState.Failed -> Text(s.message.asString(), color = LocalStatusColors.current.bad, style = MaterialTheme.typography.bodySmall)
                else -> Unit
            }
            OutlinedButton(onClick = { openKubenav(context) }, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.settings_kube_open_kubenav)) }
        }
    }
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
