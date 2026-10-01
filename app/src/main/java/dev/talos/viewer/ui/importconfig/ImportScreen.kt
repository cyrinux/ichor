package dev.talos.viewer.ui.importconfig

import android.net.Uri
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.ContentPaste
import androidx.compose.material.icons.outlined.FileOpen
import androidx.compose.material.icons.outlined.QrCodeScanner
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Tab
import androidx.compose.material3.TabRow
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import dev.talos.viewer.model.ConfigSummary
import dev.talos.viewer.ui.app
import dev.talos.viewer.ui.components.InfoRow
import dev.talos.viewer.ui.factory
import dev.talos.viewer.ui.theme.LocalStatusColors
import dev.talos.viewer.util.daysUntil
import dev.talos.viewer.util.readBounded

private const val MAX_CONFIG_BYTES = 256 * 1024

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ImportScreen(
    onImported: () -> Unit,
    vm: ImportViewModel = viewModel(factory = factory { ImportViewModel(app.configRepository) }),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    // Hoisted so the chosen tab and pasted text survive the Validating -> Invalid round trip.
    var tab by rememberSaveable { mutableIntStateOf(0) }
    // Not saveable: it may contain the client private key, which must not land in saved instance state.
    var pasted by remember { mutableStateOf("") }

    LaunchedEffect(state) {
        if (state is ImportState.Saved) onImported()
    }

    Scaffold(topBar = { TopAppBar(title = { Text("Import talosconfig") }) }) { padding ->
        Box(Modifier.padding(padding).fillMaxSize()) {
            when (val s = state) {
                is ImportState.Preview -> PreviewCard(s.summary, onConfirm = vm::confirm, onCancel = vm::reset)
                ImportState.Validating, ImportState.Saved -> Box(Modifier.fillMaxSize(), Alignment.Center) {
                    CircularProgressIndicator()
                }
                else -> SourcePicker(
                    error = (s as? ImportState.Invalid)?.message,
                    tab = tab,
                    onTab = { tab = it },
                    pasted = pasted,
                    onPasted = { pasted = it },
                    onYaml = vm::submit,
                )
            }
        }
    }
}

@Composable
private fun SourcePicker(
    error: String?,
    tab: Int,
    onTab: (Int) -> Unit,
    pasted: String,
    onPasted: (String) -> Unit,
    onYaml: (String) -> Unit,
) {
    val tabs = listOf(
        "File" to Icons.Outlined.FileOpen,
        "Paste" to Icons.Outlined.ContentPaste,
        "QR code" to Icons.Outlined.QrCodeScanner,
    )

    Column(Modifier.fillMaxSize()) {
        TabRow(selectedTabIndex = tab) {
            tabs.forEachIndexed { index, (label, icon) ->
                Tab(
                    selected = tab == index,
                    onClick = { onTab(index) },
                    text = { Text(label) },
                    icon = { Icon(icon, contentDescription = null) },
                )
            }
        }
        if (error != null) {
            Text(
                error,
                color = LocalStatusColors.current.bad,
                style = MaterialTheme.typography.bodyMedium,
                modifier = Modifier.padding(16.dp),
            )
        }
        when (tab) {
            0 -> FileSource(onYaml)
            1 -> PasteSource(pasted, onPasted, onYaml)
            else -> QrScanner(onScanned = onYaml)
        }
    }
}

@Composable
private fun FileSource(onYaml: (String) -> Unit) {
    val context = LocalContext.current
    var readError by rememberSaveable { mutableStateOf<String?>(null) }
    val picker = rememberLauncherForActivityResult(ActivityResultContracts.OpenDocument()) { uri: Uri? ->
        if (uri == null) return@rememberLauncherForActivityResult
        runCatching {
            context.contentResolver.openInputStream(uri)?.use { stream ->
                readBounded(stream, MAX_CONFIG_BYTES).decodeToString()
            } ?: error("Could not open file")
        }.fold(
            onSuccess = { readError = null; onYaml(it) },
            onFailure = { readError = it.message },
        )
    }

    Column(
        Modifier.fillMaxSize().padding(24.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text(
            "Select your talosconfig (the file at ~/.talos/config on your workstation). " +
                "Copy it to the phone with adb push, Syncthing, etc.",
            style = MaterialTheme.typography.bodyMedium,
        )
        Button(onClick = { picker.launch(arrayOf("*/*")) }) { Text("Choose file") }
        readError?.let { Text(it, color = LocalStatusColors.current.bad) }
    }
}

@Composable
private fun PasteSource(text: String, onText: (String) -> Unit, onYaml: (String) -> Unit) {
    Column(Modifier.fillMaxSize().padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        OutlinedTextField(
            value = text,
            onValueChange = { if (it.length <= MAX_CONFIG_BYTES) onText(it) },
            label = { Text("talosconfig YAML") },
            textStyle = MaterialTheme.typography.bodySmall.copy(fontFamily = FontFamily.Monospace),
            modifier = Modifier.fillMaxWidth().weight(1f),
        )
        Button(onClick = { onYaml(text) }, enabled = text.isNotBlank(), modifier = Modifier.fillMaxWidth()) {
            Text("Validate")
        }
    }
}

@Composable
private fun PreviewCard(summary: ConfigSummary, onConfirm: () -> Unit, onCancel: () -> Unit) {
    Column(
        Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text("Config is valid", style = MaterialTheme.typography.titleLarge)
        summary.contexts.forEach { ctx ->
            Card(Modifier.fillMaxWidth()) {
                Column(Modifier.padding(16.dp)) {
                    val marker = if (ctx.name == summary.current) " (current)" else ""
                    Text(ctx.name + marker, style = MaterialTheme.typography.titleMedium)
                    InfoRow("Endpoints", ctx.endpoints.joinToString("\n"), mono = true)
                    InfoRow("Nodes", if (ctx.nodes.isEmpty()) "endpoints" else "${ctx.nodes.size}")
                    InfoRow("Roles", ctx.roles.joinToString())
                    InfoRow("Cert expires", certExpiry(ctx.certNotAfter))
                }
            }
        }
        Text(
            "The config is stored encrypted on this device (Android Keystore) and excluded from backups.",
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            OutlinedButton(onClick = onCancel, modifier = Modifier.weight(1f)) { Text("Cancel") }
            Button(onClick = onConfirm, modifier = Modifier.weight(1f)) { Text("Import") }
        }
        Box(Modifier.height(8.dp))
    }
}

fun certExpiry(notAfter: Long): String {
    val days = daysUntil(notAfter)
    return if (days < 0) "expired ${-days} days ago" else "in $days days"
}
