package name.levis.talosmobile.ui.settings

import name.levis.talosmobile.R
import androidx.compose.ui.res.stringResource
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Card
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.RadioButton
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
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.compose.ui.platform.LocalContext
import name.levis.talosmobile.BuildConfig
import name.levis.talosmobile.TalosApp
import name.levis.talosmobile.data.ConfigRepository
import name.levis.talosmobile.data.TalosRepository
import name.levis.talosmobile.data.activeSummary
import name.levis.talosmobile.model.Feature
import name.levis.talosmobile.model.allows
import name.levis.talosmobile.data.UiPreferences
import name.levis.talosmobile.security.AppLock
import name.levis.talosmobile.ui.components.InfoRow
import name.levis.talosmobile.ui.components.SectionTitle
import name.levis.talosmobile.ui.importconfig.certExpiry
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
            SectionTitle(stringResource(R.string.settings_section_context))
            config?.summary?.contexts?.forEach { ctx ->
                val selected = ctx.name == config?.activeContext
                Card(
                    Modifier.fillMaxWidth().selectable(
                        selected = selected,
                        role = Role.RadioButton,
                        onClick = { configs.selectContext(ctx.name) },
                    ),
                ) {
                    Row(Modifier.padding(12.dp), verticalAlignment = Alignment.Top) {
                        RadioButton(selected = selected, onClick = null)
                        Column(Modifier.padding(start = 12.dp)) {
                            Text(ctx.name, style = MaterialTheme.typography.titleMedium)
                            InfoRow(stringResource(R.string.common_label_endpoints), ctx.endpoints.joinToString("\n"), mono = true)
                            InfoRow(stringResource(R.string.common_label_nodes), "${ctx.nodes.size.takeIf { it > 0 } ?: ctx.endpoints.size}")
                            InfoRow(stringResource(R.string.common_label_roles), ctx.roles.joinToString())
                            InfoRow(stringResource(R.string.common_label_cert_expires), certExpiry(ctx.certNotAfter))
                        }
                    }
                }
            }
            AppearanceSection(uiPreferences)
            SecuritySection(appLock, uiPreferences)
            MonitoringSection(LocalContext.current.applicationContext as TalosApp)
            UpdateSection((LocalContext.current.applicationContext as TalosApp).updateManager)
            config?.activeSummary?.takeIf { it.allows(Feature.KUBECONFIG) }?.let { KubeconfigSection(talos, appLock, it) }
            SectionTitle(stringResource(R.string.settings_section_config))
            configs.keyProtection()?.let {
                InfoRow(stringResource(R.string.settings_encryption_key), stringResource(R.string.settings_encryption_value, stringResource(it.label)))
            }
            OutlinedButton(onClick = onReimport, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.settings_import_new)) }
            OutlinedButton(onClick = { confirmDelete = true }, modifier = Modifier.fillMaxWidth()) {
                Text(stringResource(R.string.settings_delete_config))
            }
            AboutSection()
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
