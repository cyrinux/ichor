package name.levis.ichor.ui.settings

import name.levis.ichor.R
import androidx.compose.ui.res.stringResource
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import name.levis.ichor.ui.design.IchorCard
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import name.levis.ichor.ui.design.IchorOutlinedButton
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import name.levis.ichor.ui.design.IchorSwitch
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import name.levis.ichor.TalosApp
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.Feature
import name.levis.ichor.model.ImageScanner
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

private sealed interface ExportState {
    data object Idle : ExportState
    data object Working : ExportState
    data object Saved : ExportState
    data class Failed(val message: UiText) : ExportState
}

/**
 * Exports the cluster's admin kubeconfig (os:admin role), or the stored one of a cluster added
 * from a kubeconfig, to a file the user picks, for kubectl or any other Kubernetes client.
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
    IchorCard(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            // A cluster added from a kubeconfig exports that kubeconfig, not one Talos issues.
            MutedText(stringResource(if (talosContext.isKube) R.string.settings_kube_desc_imported else R.string.settings_kube_desc))
            if (!talosContext.allows(Feature.KUBECONFIG)) {
                RoleNotice(Feature.KUBECONFIG, talosContext.roles)
            } else IchorOutlinedButton(
                onClick = ::export,
                enabled = state != ExportState.Working,
                modifier = Modifier.fillMaxWidth(),
            ) { Text(stringResource(if (state == ExportState.Working) R.string.settings_kube_exporting else R.string.settings_kube_export)) }
            when (val s = state) {
                ExportState.Saved -> Text(stringResource(R.string.settings_kube_saved), style = MaterialTheme.typography.bodySmall)
                is ExportState.Failed -> Text(s.message.asString(), color = LocalStatusColors.current.bad, style = MaterialTheme.typography.bodySmall)
                else -> Unit
            }
        }
    }
    NodeDebugShellSetting(appLock)
    ImageScannerSetting()
}

/** The scanner an image scan runs in the cluster: Grype (the default) or Trivy. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun ImageScannerSetting() {
    val prefs = (LocalContext.current.applicationContext as TalosApp).uiPreferences
    val selected by prefs.imageScanner.collectAsStateWithLifecycle()

    IchorCard(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(stringResource(R.string.settings_image_scanner), style = MaterialTheme.typography.titleMedium)
            MutedText(stringResource(R.string.settings_image_scanner_desc))
            SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth()) {
                ImageScanner.entries.forEachIndexed { index, option ->
                    SegmentedButton(
                        selected = option == selected,
                        onClick = { prefs.setImageScanner(option) },
                        shape = SegmentedButtonDefaults.itemShape(index, ImageScanner.entries.size),
                    ) { Text(option.label) }
                }
            }
        }
    }
}

/**
 * Opt-in: a root shell on the nodes of a cluster without Talos, through a privileged pod.
 * With the app lock on, turning it on asks for a fresh fingerprint/PIN.
 */
@Composable
private fun NodeDebugShellSetting(appLock: AppLock) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val prefs = (context.applicationContext as TalosApp).uiPreferences
    val enabled by prefs.nodeDebugShell.collectAsStateWithLifecycle()
    var error by remember { mutableStateOf<String?>(null) }

    fun set(on: Boolean) {
        error = null
        val activity = context.findFragmentActivity()
        if (!on || !appLock.enabled.value || activity == null) {
            prefs.setNodeDebugShell(on)
            return
        }
        scope.launch {
            when (val auth = authenticate(activity, context.getString(R.string.settings_node_debug_shell))) {
                AuthResult.Success -> prefs.setNodeDebugShell(true)
                is AuthResult.Failure -> error = auth.message
            }
        }
    }

    IchorCard(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) {
                    Text(stringResource(R.string.settings_node_debug_shell), style = MaterialTheme.typography.titleMedium)
                    MutedText(stringResource(R.string.settings_node_debug_shell_desc))
                }
                IchorSwitch(checked = enabled, onCheckedChange = ::set, modifier = Modifier.padding(start = 12.dp))
            }
            error?.let { Text(it, color = LocalStatusColors.current.bad, style = MaterialTheme.typography.bodySmall) }
        }
    }
}
