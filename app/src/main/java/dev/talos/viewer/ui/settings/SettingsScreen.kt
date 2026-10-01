package dev.talos.viewer.ui.settings

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
import dev.talos.viewer.BuildConfig
import dev.talos.viewer.TalosApp
import dev.talos.viewer.data.ConfigRepository
import dev.talos.viewer.data.TalosRepository
import dev.talos.viewer.data.activeSummary
import dev.talos.viewer.data.UiPreferences
import dev.talos.viewer.security.AppLock
import dev.talos.viewer.ui.components.InfoRow
import dev.talos.viewer.ui.components.SectionTitle
import dev.talos.viewer.ui.importconfig.certExpiry
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
                title = { Text("Settings") },
                navigationIcon = { IconButton(onClick = onBack) { Icon(Icons.AutoMirrored.Filled.ArrowBack, "Back") } },
            )
        },
    ) { padding ->
        Column(
            Modifier.padding(padding).fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            SectionTitle("Context")
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
                            InfoRow("Endpoints", ctx.endpoints.joinToString("\n"), mono = true)
                            InfoRow("Nodes", "${ctx.nodes.size.takeIf { it > 0 } ?: ctx.endpoints.size}")
                            InfoRow("Roles", ctx.roles.joinToString())
                            InfoRow("Cert expires", certExpiry(ctx.certNotAfter))
                        }
                    }
                }
            }
            AppearanceSection(uiPreferences)
            SecuritySection(appLock, uiPreferences)
            MonitoringSection(LocalContext.current.applicationContext as TalosApp)
            UpdateSection((LocalContext.current.applicationContext as TalosApp).updateManager)
            config?.activeSummary?.let { KubeconfigSection(talos, appLock, it) }
            SectionTitle("Config")
            OutlinedButton(onClick = onReimport, modifier = Modifier.fillMaxWidth()) { Text("Import a new talosconfig") }
            OutlinedButton(onClick = { confirmDelete = true }, modifier = Modifier.fillMaxWidth()) {
                Text("Delete stored talosconfig")
            }
            Text(
                "Talos Viewer ${BuildConfig.VERSION_NAME} (${BuildConfig.VERSION_CODE})",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(top = 8.dp),
            )
        }
    }

    if (confirmDelete) {
        AlertDialog(
            onDismissRequest = { confirmDelete = false },
            title = { Text("Delete talosconfig?") },
            text = { Text("The encrypted config and its client key will be removed from this device.") },
            confirmButton = {
                TextButton(onClick = {
                    confirmDelete = false
                    scope.launch {
                        configs.clear()
                        onCleared()
                    }
                }) { Text("Delete") }
            },
            dismissButton = { TextButton(onClick = { confirmDelete = false }) { Text("Cancel") } },
        )
    }
}
