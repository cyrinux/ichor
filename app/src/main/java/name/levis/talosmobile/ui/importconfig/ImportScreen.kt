package name.levis.talosmobile.ui.importconfig

import name.levis.talosmobile.ui.uiText
import name.levis.talosmobile.ui.UiText
import name.levis.talosmobile.ui.LocalizedException
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import name.levis.talosmobile.R
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
import androidx.compose.material3.IconButton
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.automirrored.outlined.HelpOutline
import androidx.compose.ui.platform.LocalContext
import name.levis.talosmobile.TalosApp
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
import name.levis.talosmobile.model.ConfigSummary
import name.levis.talosmobile.ui.app
import name.levis.talosmobile.ui.components.InfoRow
import name.levis.talosmobile.ui.factory
import name.levis.talosmobile.ui.theme.LocalStatusColors
import name.levis.talosmobile.util.daysUntil
import name.levis.talosmobile.util.readBounded

private const val MAX_CONFIG_BYTES = 256 * 1024

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ImportScreen(
    onImported: () -> Unit,
    onBack: (() -> Unit)? = null,
    vm: ImportViewModel = viewModel(factory = factory { ImportViewModel(app.configRepository) }),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    // First run (nothing imported yet): explain how to create a talosconfig right away.
    val firstRun = app().configRepository.config.collectAsStateWithLifecycle().value == null
    var showHelp by rememberSaveable { mutableStateOf(firstRun) }
    // Hoisted so the chosen tab and pasted text survive the Validating -> Invalid round trip.
    var tab by rememberSaveable { mutableIntStateOf(0) }
    // Not saveable: it may contain the client private key, which must not land in saved instance state.
    var pasted by remember { mutableStateOf("") }

    LaunchedEffect(state) {
        if (state is ImportState.Saved) onImported()
    }

    if (showHelp) TalosconfigHelpDialog(onDismiss = { showHelp = false })

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text(stringResource(R.string.import_title)) },
                navigationIcon = {
                    onBack?.let {
                        IconButton(onClick = it) { Icon(Icons.AutoMirrored.Filled.ArrowBack, stringResource(R.string.common_back)) }
                    }
                },
                actions = {
                    IconButton(onClick = { showHelp = true }) {
                        Icon(Icons.AutoMirrored.Outlined.HelpOutline, contentDescription = stringResource(R.string.import_help))
                    }
                },
            )
        },
    ) { padding ->
        Box(Modifier.padding(padding).fillMaxSize()) {
            when (val s = state) {
                is ImportState.Preview -> PreviewCard(s.summary, adding = !firstRun, onConfirm = vm::confirm, onCancel = vm::reset)
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
        stringResource(R.string.import_tab_file) to Icons.Outlined.FileOpen,
        stringResource(R.string.import_tab_paste) to Icons.Outlined.ContentPaste,
        stringResource(R.string.import_tab_qr) to Icons.Outlined.QrCodeScanner,
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
            } ?: throw LocalizedException(UiText.Res(R.string.import_could_not_open))
        }.fold(
            onSuccess = { readError = null; onYaml(it) },
            onFailure = {
                // readBounded rejects oversized files with IllegalArgumentException.
                readError = if (it is IllegalArgumentException) {
                    context.getString(R.string.import_file_too_large)
                } else {
                    it.uiText().resolve(context)
                }
            },
        )
    }

    Column(
        Modifier.fillMaxSize().padding(24.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text(
            stringResource(R.string.import_file_hint),
            style = MaterialTheme.typography.bodyMedium,
        )
        Button(onClick = { picker.launch(arrayOf("*/*")) }) { Text(stringResource(R.string.import_choose_file)) }
        readError?.let { Text(it, color = LocalStatusColors.current.bad) }
    }
}

@Composable
private fun PasteSource(text: String, onText: (String) -> Unit, onYaml: (String) -> Unit) {
    Column(Modifier.fillMaxSize().padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        OutlinedTextField(
            value = text,
            onValueChange = { if (it.length <= MAX_CONFIG_BYTES) onText(it) },
            label = { Text(stringResource(R.string.import_paste_label)) },
            textStyle = MaterialTheme.typography.bodySmall.copy(fontFamily = FontFamily.Monospace),
            modifier = Modifier.fillMaxWidth().weight(1f),
        )
        Button(onClick = { onYaml(text) }, enabled = text.isNotBlank(), modifier = Modifier.fillMaxWidth()) {
            Text(stringResource(R.string.import_validate))
        }
    }
}

@Composable
private fun PreviewCard(summary: ConfigSummary, adding: Boolean, onConfirm: () -> Unit, onCancel: () -> Unit) {
    Column(
        Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text(stringResource(R.string.import_valid), style = MaterialTheme.typography.titleLarge)
        summary.contexts.forEach { ctx ->
            Card(Modifier.fillMaxWidth()) {
                Column(Modifier.padding(16.dp)) {
                    val name = if (ctx.name == summary.current) stringResource(R.string.import_context_current, ctx.name) else ctx.name
                    Text(name, style = MaterialTheme.typography.titleMedium)
                    InfoRow(stringResource(R.string.common_label_endpoints), ctx.endpoints.joinToString("\n"), mono = true)
                    InfoRow(stringResource(R.string.common_label_nodes), if (ctx.nodes.isEmpty()) stringResource(R.string.import_nodes_endpoints) else "${ctx.nodes.size}")
                    InfoRow(stringResource(R.string.common_label_roles), ctx.roles.joinToString())
                    InfoRow(stringResource(R.string.common_label_cert_expires), certExpiry(ctx.certNotAfter))
                }
            }
        }
        // The clusters already imported stay: say what this import does to them.
        if (adding) Text(stringResource(R.string.import_adds_cluster), style = MaterialTheme.typography.bodyMedium)
        Text(
            stringResource(R.string.import_stored_encrypted),
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            OutlinedButton(onClick = onCancel, modifier = Modifier.weight(1f)) { Text(stringResource(R.string.common_cancel)) }
            Button(onClick = onConfirm, modifier = Modifier.weight(1f)) { Text(stringResource(R.string.import_import)) }
        }
        Box(Modifier.height(8.dp))
    }
}

@Composable
fun certExpiry(notAfter: Long): String {
    val days = daysUntil(notAfter).toInt()
    return if (days < 0) {
        pluralStringResource(R.plurals.common_cert_expired_days_ago, -days, -days)
    } else {
        pluralStringResource(R.plurals.common_cert_in_days, days, days)
    }
}

@Composable
private fun app(): TalosApp = LocalContext.current.applicationContext as TalosApp
