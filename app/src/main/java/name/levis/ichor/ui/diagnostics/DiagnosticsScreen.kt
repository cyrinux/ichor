package name.levis.ichor.ui.diagnostics

import android.app.Application
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Archive
import androidx.compose.material.icons.outlined.DeleteSweep
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Card
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.model.DiagEntry
import name.levis.ichor.model.DiagnosticsView
import name.levis.ichor.model.ExitRecord
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.ConfirmDialog
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.pageContent
import name.levis.ichor.ui.components.rememberSaveFile
import name.levis.ichor.ui.components.shareFile
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.formatBytes
import name.levis.ichor.util.formatDateTime

private const val ZIP_MIME = "application/zip"

/**
 * Settings → Support → Export diagnostics: the OS's crash and exit records of the app, then
 * the newest entries of the core's redacted log. Export writes the zip, which the user then
 * shares or saves themself: nothing is sent by the app.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DiagnosticsScreen(onBack: () -> Unit) {
    val appContext = LocalContext.current.applicationContext as Application
    val vm: DiagnosticsViewModel = viewModel(factory = factory { DiagnosticsViewModel(appContext) })
    val state by vm.state.collectAsStateWithLifecycle()
    val export by vm.export.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { vm.refresh() }
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val snackbar = remember { SnackbarHostState() }
    var confirmClear by remember { mutableStateOf(false) }
    val save = rememberSaveFile(ZIP_MIME, R.string.diagnostics_saved, R.string.diagnostics_save_failed) { message ->
        scope.launch { snackbar.showSnackbar(message.resolve(context)) }
    }
    val entries = (state as? UiState.Loaded)?.data?.entries.orEmpty()

    Scaffold(
        snackbarHost = { SnackbarHost(snackbar) },
        topBar = {
            TopAppBar(
                title = { Text(stringResource(R.string.diagnostics_title)) },
                navigationIcon = { BackButton(onBack) },
                actions = {
                    TooltipIconButton(Icons.Outlined.Archive, stringResource(R.string.diagnostics_export), onClick = vm::exportZip)
                    TooltipIconButton(
                        Icons.Outlined.DeleteSweep,
                        stringResource(R.string.diagnostics_clear),
                        enabled = entries.isNotEmpty(),
                        onClick = { confirmClear = true },
                    )
                },
            )
        },
    ) { padding ->
        when (val s = state) {
            UiState.Loading -> LoadingBox(Modifier.pageContent(padding))
            is UiState.Failed -> ErrorBox(s.message, { vm.refresh() }, Modifier.pageContent(padding))
            is UiState.Loaded -> DiagnosticsList(s.data, Modifier.pageContent(padding))
        }
    }

    export?.let { current ->
        ExportDialog(
            current,
            onShare = { file -> shareFile(context, file, ZIP_MIME, R.string.diagnostics_share_chooser) },
            onSave = { file -> save(file) },
            onDismiss = vm::dismissExport,
        )
    }

    if (confirmClear) {
        ConfirmDialog(
            title = stringResource(R.string.diagnostics_clear_title),
            text = stringResource(R.string.diagnostics_clear_body),
            confirm = stringResource(R.string.diagnostics_clear),
            destructive = true,
            onConfirm = {
                confirmClear = false
                vm.clear()
            },
            onDismiss = { confirmClear = false },
        )
    }
}

@Composable
private fun DiagnosticsList(view: DiagnosticsView, modifier: Modifier) {
    LazyColumn(
        modifier.fillMaxSize(),
        contentPadding = PaddingValues(16.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        item { MutedText(stringResource(R.string.diagnostics_zip_note)) }
        item { SectionTitle(stringResource(R.string.diagnostics_exits)) }
        when {
            !view.exits.supported -> item { MutedText(stringResource(R.string.diagnostics_exits_unsupported)) }
            view.exits.exits.isEmpty() -> item { MutedText(stringResource(R.string.diagnostics_exits_empty)) }
            else -> itemsIndexed(view.exits.exits, key = { i, e -> "exit|${e.at}|$i" }) { _, exit -> ExitCard(exit) }
        }
        item { SectionTitle(stringResource(R.string.diagnostics_log)) }
        if (view.entries.isEmpty()) {
            item { EmptyText(stringResource(R.string.diagnostics_log_empty)) }
        }
        itemsIndexed(view.entries, key = { i, e -> "log|${e.at}|$i" }) { _, entry ->
            EntryRow(entry)
            HorizontalDivider()
        }
    }
}

@Composable
private fun ExitCard(exit: ExitRecord) {
    var showTrace by rememberSaveable(exit.at) { mutableStateOf(false) }
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text(reasonLabel(exit.reason), style = MaterialTheme.typography.titleSmall)
            MutedText(formatDateTime(exit.at))
            if (exit.description.isNotEmpty()) Text(exit.description, style = MaterialTheme.typography.bodySmall)
            MutedText(stringResource(R.string.diagnostics_exit_details, exit.status, exit.importance, formatBytes(exit.pss * 1024)))
            if (exit.trace.isNotEmpty()) {
                TextButton(onClick = { showTrace = !showTrace }) {
                    Text(stringResource(if (showTrace) R.string.diagnostics_trace_hide else R.string.diagnostics_trace_show))
                }
                if (showTrace) {
                    SelectionContainer {
                        Text(
                            exit.trace,
                            style = MaterialTheme.typography.bodySmall,
                            fontFamily = FontFamily.Monospace,
                            modifier = Modifier.horizontalScroll(rememberScrollState()),
                        )
                    }
                }
            }
        }
    }
}

@Composable
private fun EntryRow(entry: DiagEntry) {
    val status = LocalStatusColors.current
    val levelColor = when (entry.level) {
        "error" -> status.bad
        "warn" -> status.warn
        else -> MaterialTheme.colorScheme.onSurfaceVariant
    }
    Column(Modifier.padding(vertical = 6.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Text(entry.msg, style = MaterialTheme.typography.bodyMedium)
        Text(
            listOf(formatDateTime(entry.at), entry.level, entry.component).joinToString(" · "),
            style = MaterialTheme.typography.labelSmall,
            color = levelColor,
        )
        if (entry.attrs.isNotEmpty()) {
            MutedText(entry.attrs.entries.joinToString(" ") { (k, v) -> "$k=${v.content}" })
        }
        if (entry.err.isNotEmpty()) Text(entry.err, style = MaterialTheme.typography.bodySmall, color = status.bad)
    }
}

/** "crash_native" as "Crash native": the reason names are plain English words. */
private fun reasonLabel(reason: String): String = reason.replace('_', ' ').replaceFirstChar { it.uppercase() }

/** The export in progress, the zip ready (Share, Save…), or why it failed. */
@Composable
private fun ExportDialog(export: DiagExport, onShare: (java.io.File) -> Unit, onSave: (java.io.File) -> Unit, onDismiss: () -> Unit) {
    val ready = export as? DiagExport.Ready
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.diagnostics_export)) },
        text = {
            when (export) {
                DiagExport.Running -> Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    Text(stringResource(R.string.diagnostics_exporting))
                    LinearProgressIndicator(Modifier.fillMaxWidth())
                }
                is DiagExport.Ready ->
                    Text(stringResource(R.string.diagnostics_ready, export.file.name, formatBytes(export.bytes), export.entries))
                is DiagExport.Failed ->
                    Text(stringResource(R.string.diagnostics_export_failed, export.message.asString()), color = LocalStatusColors.current.bad)
            }
        },
        confirmButton = {
            ready?.let { r ->
                TextButton(onClick = { onShare(r.file) }) { Text(stringResource(R.string.diagnostics_share)) }
            }
        },
        dismissButton = {
            Row {
                TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_close)) }
                ready?.let { r -> TextButton(onClick = { onSave(r.file) }) { Text(stringResource(R.string.diagnostics_save)) } }
            }
        },
    )
}
