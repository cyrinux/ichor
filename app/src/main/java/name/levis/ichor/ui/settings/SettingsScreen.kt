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
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
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
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.data.activeSummary
import name.levis.ichor.model.Feature
import name.levis.ichor.model.TalosFeature
import name.levis.ichor.model.notice
import name.levis.ichor.ui.components.InfoNotice
import name.levis.ichor.ui.components.rememberClusterSupport
import name.levis.ichor.ui.components.text
import name.levis.ichor.model.allows
import name.levis.ichor.data.UiPreferences
import name.levis.ichor.security.AppLock
import name.levis.ichor.security.lockRequired
import name.levis.ichor.ui.backup.BackupSection
import name.levis.ichor.ui.components.InfoRow
import name.levis.ichor.ui.components.SectionTitle
import kotlinx.coroutines.launch

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
    onChangelog: () -> Unit,
    onLicenses: () -> Unit,
    onCleared: () -> Unit,
) {
    val config by configs.config.collectAsStateWithLifecycle()
    val scope = rememberCoroutineScope()
    var confirmDelete by remember { mutableStateOf(false) }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text(stringResource(R.string.settings_title)) },
                navigationIcon = { IconButton(onClick = onBack) { Icon(Icons.AutoMirrored.Filled.ArrowBack, stringResource(R.string.common_back)) } },
            )
        },
    ) { padding ->
        Column(
            Modifier.padding(padding).fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
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
                Text(
                    stringResource(R.string.settings_issue_desc),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                val issue = rememberClusterSupport(TalosFeature.ISSUE_CONFIG)
                OutlinedButton(onClick = onIssueConfig, enabled = issue.supported, modifier = Modifier.fillMaxWidth()) {
                    Text(stringResource(R.string.settings_issue_open))
                }
                issue.notice?.let { InfoNotice(it.text()) }
            }
            configs.keyProtection()?.let {
                InfoRow(stringResource(R.string.settings_encryption_key), stringResource(R.string.settings_encryption_value, stringResource(it.label)))
            }
            OutlinedButton(onClick = onReimport, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.settings_import_new)) }
            BackupSection(hasConfig = config != null)
            OutlinedButton(onClick = { confirmDelete = true }, modifier = Modifier.fillMaxWidth()) {
                Text(stringResource(R.string.settings_delete_config))
            }
            // `talosctl support` reads logs and resources of every chosen node (any role).
            if (config?.activeSummary?.allows(Feature.SUPPORT_BUNDLE) == true) {
                SectionTitle(stringResource(R.string.support_bundle_title))
                Text(
                    stringResource(R.string.settings_support_bundle_desc),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                val support = rememberClusterSupport(TalosFeature.SUPPORT_BUNDLE)
                OutlinedButton(onClick = onSupportBundle, enabled = support.supported, modifier = Modifier.fillMaxWidth()) {
                    Text(stringResource(R.string.settings_support_bundle_open))
                }
                support.notice?.let { InfoNotice(it.text()) }
            }
            AboutSection(onChangelog, onLicenses)
            if (BuildConfig.SELF_UPDATE) UpdateSection((LocalContext.current.applicationContext as TalosApp).updateManager)
        }
    }

    if (confirmDelete) {
        AlertDialog(
            onDismissRequest = { confirmDelete = false },
            title = { Text(stringResource(R.string.settings_delete_title)) },
            text = { Text(stringResource(R.string.settings_delete_body)) },
            confirmButton = {
                TextButton(onClick = {
                    confirmDelete = false
                    scope.launch {
                        configs.clear()
                        onCleared()
                    }
                }) { Text(stringResource(R.string.common_delete)) }
            },
            dismissButton = { TextButton(onClick = { confirmDelete = false }) { Text(stringResource(R.string.common_cancel)) } },
        )
    }
}
