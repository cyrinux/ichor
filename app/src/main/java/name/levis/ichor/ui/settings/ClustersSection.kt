package name.levis.ichor.ui.settings

import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.ConfigRepository
import name.levis.ichor.data.KeyProtection
import name.levis.ichor.data.activeSummary
import name.levis.ichor.model.Feature
import name.levis.ichor.model.TalosFeature
import name.levis.ichor.model.allows
import name.levis.ichor.model.isDemo
import name.levis.ichor.model.notice
import name.levis.ichor.ui.backup.BackupSection
import name.levis.ichor.ui.backup.backupViewModel
import name.levis.ichor.ui.backup.rememberBackupAction
import name.levis.ichor.ui.components.InfoNotice
import name.levis.ichor.ui.components.InfoRow
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.rememberClusterSupport
import name.levis.ichor.ui.components.text
import name.levis.ichor.ui.node.HostnameConfirmDialog

/**
 * Settings → Clusters & backup: the encryption key, adding a cluster, renewing the certificate or
 * issuing a config for another device (os:admin), back up / restore, and deleting every cluster.
 */
@Composable
fun ClustersSection(configs: ConfigRepository, onReimport: () -> Unit, onIssueConfig: () -> Unit, onRotateCA: () -> Unit, onCleared: () -> Unit) {
    val config by configs.config.collectAsStateWithLifecycle()
    // Keystore calls: read off the main thread, again only when the stored config changes.
    val keyProtection by produceState<KeyProtection?>(null, config) { value = withContext(Dispatchers.IO) { configs.keyProtection() } }
    val scope = rememberCoroutineScope()
    var confirmDelete by remember { mutableStateOf(false) }
    var onCallOpen by remember { mutableStateOf(false) }
    val backupVm = backupViewModel()
    val backup = rememberBackupAction(backupVm)

    SectionTitle(stringResource(R.string.settings_section_config))
    if (config?.activeSummary?.allows(Feature.ISSUE_CONFIG) == true) {
        MutedText(stringResource(R.string.settings_issue_desc))
        val issue = rememberClusterSupport(TalosFeature.ISSUE_CONFIG)
        OutlinedButton(onClick = onIssueConfig, enabled = issue.supported, modifier = Modifier.fillMaxWidth()) {
            Text(stringResource(R.string.settings_issue_open))
        }
        issue.notice?.let { InfoNotice(it.text()) }
        // Omni holds an Omni cluster's CA: Go refuses it with the reason, the button stays.
        OutlinedButton(onClick = onRotateCA, enabled = issue.supported, modifier = Modifier.fillMaxWidth()) {
            Text(stringResource(R.string.settings_rotate_ca_open))
        }
    }
    keyProtection?.let {
        InfoRow(stringResource(R.string.settings_encryption_key), stringResource(R.string.settings_encryption_value, stringResource(it.label)))
    }
    OutlinedButton(onClick = onReimport, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.settings_import_new)) }
    // The demo has its sample incidents without settings.
    config?.activeSummary?.takeUnless { it.isDemo }?.fingerprint?.takeIf { it.isNotBlank() }?.let { fingerprint ->
        MutedText(stringResource(R.string.oncall_row_desc))
        OutlinedButton(onClick = { onCallOpen = true }, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.oncall_title)) }
        if (onCallOpen) OnCallSheet(LocalContext.current.applicationContext as TalosApp, fingerprint) { onCallOpen = false }
    }
    BackupSection(hasConfig = config != null, vm = backupVm)
    OutlinedButton(onClick = { confirmDelete = true }, modifier = Modifier.fillMaxWidth()) {
        Text(stringResource(R.string.settings_delete_config))
    }

    // Every cluster's client keys go, for good without a backup: typed, like a reboot.
    if (confirmDelete) {
        HostnameConfirmDialog(
            title = stringResource(R.string.settings_delete_title),
            hostname = DELETE_ALL_TOKEN,
            confirmLabel = stringResource(R.string.common_delete),
            onConfirm = {
                confirmDelete = false
                scope.launch {
                    configs.clear()
                    onCleared()
                }
            },
            onDismiss = { confirmDelete = false },
        ) {
            Text(stringResource(R.string.settings_delete_body))
            if (config != null) {
                TextButton(onClick = {
                    confirmDelete = false
                    backup()
                }) { Text(stringResource(R.string.settings_delete_backup_first)) }
            }
        }
    }
}

/** What to type to delete every cluster; not translated, like a hostname. */
private const val DELETE_ALL_TOKEN = "DELETE"
