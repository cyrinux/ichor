package name.levis.ichor.ui.settings

import name.levis.ichor.R
import androidx.compose.ui.res.stringResource
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.compose.ui.platform.LocalContext
import name.levis.ichor.BuildConfig
import name.levis.ichor.TalosApp
import name.levis.ichor.data.ConfigRepository
import name.levis.ichor.data.KeyProtection
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.data.activeSummary
import name.levis.ichor.model.Feature
import name.levis.ichor.model.TalosFeature
import name.levis.ichor.model.notice
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.backup.backupViewModel
import name.levis.ichor.ui.backup.rememberBackupAction
import name.levis.ichor.ui.node.HostnameConfirmDialog
import androidx.compose.material3.TextButton
import name.levis.ichor.ui.components.InfoNotice
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.rememberClusterSupport
import name.levis.ichor.ui.components.text
import name.levis.ichor.model.allows
import name.levis.ichor.data.UiPreferences
import name.levis.ichor.security.AppLock
import name.levis.ichor.security.lockRequired
import name.levis.ichor.ui.backup.BackupSection
import name.levis.ichor.ui.components.InfoRow
import name.levis.ichor.ui.components.SectionTitle
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import name.levis.ichor.data.REPO_URL_BASE
import name.levis.ichor.ui.components.pageContent

/** GitHub's issue chooser: bug report or integration request. */
private val REPORT_BUG_URL = "$REPO_URL_BASE${BuildConfig.UPDATE_REPO}/issues/new/choose"

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SettingsScreen(
    configs: ConfigRepository,
    uiPreferences: UiPreferences,
    appLock: AppLock,
    talos: TalosRepository,
    onBack: () -> Unit,
    onReimport: () -> Unit,
    onIssueConfig: () -> Unit,
    onSupportBundle: () -> Unit,
    onActivity: () -> Unit,
    onIntegrations: () -> Unit,
    onChangelog: () -> Unit,
    onLicenses: () -> Unit,
    onSupportedIntegrations: () -> Unit,
    onFunding: () -> Unit,
    onCleared: () -> Unit,
) {
    val config by configs.config.collectAsStateWithLifecycle()
    // Keystore calls: read off the main thread, again only when the stored config changes.
    val keyProtection by produceState<KeyProtection?>(null, config) { value = withContext(Dispatchers.IO) { configs.keyProtection() } }
    val scope = rememberCoroutineScope()
    var confirmDelete by remember { mutableStateOf(false) }
    val backupVm = backupViewModel()
    val backup = rememberBackupAction(backupVm)

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text(stringResource(R.string.settings_title)) },
                navigationIcon = { BackButton(onBack) },
            )
        },
    ) { padding ->
        Column(
            Modifier.pageContent(padding).fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            AppearanceSection(uiPreferences)
            SecuritySection(appLock, uiPreferences, required = lockRequired(config?.summary?.contexts.orEmpty()))
            PrivacySection(LocalContext.current.applicationContext as TalosApp)
            MonitoringSection(LocalContext.current.applicationContext as TalosApp)
            AiSection(LocalContext.current.applicationContext as TalosApp)
            config?.activeSummary?.takeIf { it.allows(Feature.KUBECONFIG) }?.let { KubeconfigSection(talos, appLock, it) }
            SectionTitle(stringResource(R.string.settings_section_config))
            // Renewing the certificate or issuing a config for another device (os:admin).
            if (config?.activeSummary?.allows(Feature.ISSUE_CONFIG) == true) {
                MutedText(stringResource(R.string.settings_issue_desc))
                val issue = rememberClusterSupport(TalosFeature.ISSUE_CONFIG)
                OutlinedButton(onClick = onIssueConfig, enabled = issue.supported, modifier = Modifier.fillMaxWidth()) {
                    Text(stringResource(R.string.settings_issue_open))
                }
                issue.notice?.let { InfoNotice(it.text()) }
            }
            keyProtection?.let {
                InfoRow(stringResource(R.string.settings_encryption_key), stringResource(R.string.settings_encryption_value, stringResource(it.label)))
            }
            OutlinedButton(onClick = onReimport, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.settings_import_new)) }
            BackupSection(hasConfig = config != null, vm = backupVm)
            OutlinedButton(onClick = { confirmDelete = true }, modifier = Modifier.fillMaxWidth()) {
                Text(stringResource(R.string.settings_delete_config))
            }
            SectionTitle(stringResource(R.string.settings_section_support))
            // `talosctl support` reads logs and resources of every chosen node (any role).
            if (config?.activeSummary?.allows(Feature.SUPPORT_BUNDLE) == true) {
                MutedText(stringResource(R.string.settings_support_bundle_desc))
                val support = rememberClusterSupport(TalosFeature.SUPPORT_BUNDLE)
                OutlinedButton(onClick = onSupportBundle, enabled = support.supported, modifier = Modifier.fillMaxWidth()) {
                    Text(stringResource(R.string.settings_support_bundle_open))
                }
                support.notice?.let { InfoNotice(it.text()) }
            }
            // On this device only, whatever the cluster: no role needed.
            MutedText(stringResource(R.string.activity_settings_desc))
            OutlinedButton(onClick = onActivity, modifier = Modifier.fillMaxWidth()) {
                Text(stringResource(R.string.activity_open))
            }
            // Listing the cluster's API groups needs the admin kubeconfig.
            if (config?.activeSummary?.allows(Feature.WORKLOADS) == true) {
                MutedText(stringResource(R.string.settings_integrations_desc))
                OutlinedButton(onClick = onIntegrations, modifier = Modifier.fillMaxWidth()) {
                    Text(stringResource(R.string.settings_integrations_open))
                }
            }
            val context = LocalContext.current
            OutlinedButton(onClick = { openUrl(context, REPORT_BUG_URL) }, modifier = Modifier.fillMaxWidth()) {
                Text(stringResource(R.string.settings_report_bug))
            }
            AboutSection(onChangelog, onLicenses, onSupportedIntegrations, onFunding)
            if (BuildConfig.SELF_UPDATE) UpdateSection((LocalContext.current.applicationContext as TalosApp).updateManager)
        }
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
