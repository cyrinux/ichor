package name.levis.ichor.ui.importconfig

import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.MutedText
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import name.levis.ichor.R
import androidx.activity.compose.BackHandler
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
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material.icons.automirrored.outlined.HelpOutline
import androidx.compose.ui.platform.LocalContext
import name.levis.ichor.TalosApp
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.model.ImportChoice
import name.levis.ichor.model.ImportConflict
import name.levis.ichor.ui.app
import name.levis.ichor.ui.backup.RestoreBackupButton
import name.levis.ichor.ui.components.InfoRow
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.daysUntil
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.pageContent

// As the Go core's limit on a decoded "ichor-config:" payload, and iOS.
internal const val MAX_CONFIG_BYTES = 1 shl 20

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ImportScreen(
    onImported: () -> Unit,
    onBack: (() -> Unit)? = null,
    autoStartDemo: Boolean = false,
    incoming: String? = null,
    onIncomingTaken: () -> Unit = {},
    vm: ImportViewModel = viewModel(factory = factory { ImportViewModel(app.configRepository, app.kubeAuthRepository) }),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    // Keep the demo entry visible on first launch; config help is available in the toolbar.
    val firstRun = app().configRepository.config.collectAsStateWithLifecycle().value == null
    var showHelp by rememberSaveable { mutableStateOf(false) }
    // Hoisted so the chosen source and pasted text survive the Validating -> Invalid round trip.
    var source by rememberSaveable { mutableStateOf(ImportSource.PICK) }
    // In the paste or QR view: back returns to the drop zone, not out of the screen.
    val inSource = source != ImportSource.PICK && (state is ImportState.Idle || state is ImportState.Invalid)
    BackHandler(enabled = inSource) { source = ImportSource.PICK }
    // Not saveable: it may contain the client private key, which must not land in saved instance state.
    var pasted by remember { mutableStateOf("") }

    LaunchedEffect(autoStartDemo) {
        if (autoStartDemo && state is ImportState.Idle) vm.startDemo()
    }

    // A config opened with the app (a file manager, a mail): straight to its preview.
    LaunchedEffect(incoming) {
        val text = incoming ?: return@LaunchedEffect
        vm.submit(text)
        onIncomingTaken()
    }

    LaunchedEffect(state) {
        if (state is ImportState.Saved) onImported()
    }

    if (showHelp) TalosconfigHelpDialog(onDismiss = { showHelp = false })

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text(stringResource(R.string.import_title)) },
                navigationIcon = {
                    if (inSource) {
                        BackButton { source = ImportSource.PICK }
                    } else {
                        onBack?.let { BackButton(it) }
                    }
                },
                actions = {
                    TooltipIconButton(Icons.AutoMirrored.Outlined.HelpOutline, stringResource(R.string.import_help), onClick = { showHelp = true })
                },
            )
        },
    ) { padding ->
        Box(Modifier.pageContent(padding).fillMaxSize()) {
            when (val s = state) {
                is ImportState.Preview -> PreviewCard(
                    s,
                    adding = !firstRun,
                    onRename = vm::rename,
                    onReplace = vm::setReplace,
                    onConfirm = vm::confirm,
                    onCancel = vm::reset,
                )
                is ImportState.KubePreview -> KubePreviewCard(
                    s,
                    adding = !firstRun,
                    onInclude = vm::setIncluded,
                    onRename = vm::rename,
                    onReplace = vm::setReplace,
                    onConfirm = vm::confirm,
                    onCancel = vm::reset,
                )
                is ImportState.Discover -> DiscoverCard(s, onDiscover = vm::discover, onCancel = vm::reset)
                ImportState.Validating, ImportState.Saved -> Box(Modifier.fillMaxSize(), Alignment.Center) {
                    CircularProgressIndicator()
                }
                else -> SourcePicker(
                    source = source,
                    error = (s as? ImportState.Invalid)?.message,
                    pasted = pasted,
                    onPasted = { pasted = it },
                    onSource = { source = it },
                    onYaml = vm::submit,
                    onDemo = vm::startDemo,
                    onDiscover = vm::startDiscovery,
                    // A restore replaces every cluster: offered when there is none yet.
                    restore = if (firstRun) ({ RestoreBackupButton(onRestored = onImported) }) else null,
                )
            }
        }
    }
}

@Composable
private fun PreviewCard(
    preview: ImportState.Preview,
    adding: Boolean,
    onRename: (Int, String) -> Unit,
    onReplace: (Int, Boolean) -> Unit,
    onConfirm: () -> Unit,
    onCancel: () -> Unit,
) {
    val summary = preview.summary
    Column(
        Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text(stringResource(R.string.import_valid), style = MaterialTheme.typography.titleLarge)
        summary.contexts.forEachIndexed { index, ctx ->
            Card(Modifier.fillMaxWidth()) {
                Column(Modifier.padding(16.dp)) {
                    val name = if (ctx.name == summary.current) stringResource(R.string.import_context_current, ctx.name) else ctx.name
                    Text(name, style = MaterialTheme.typography.titleMedium)
                    InfoRow(stringResource(R.string.common_label_endpoints), ctx.endpoints.joinToString("\n"), mono = true)
                    InfoRow(stringResource(R.string.common_label_nodes), if (ctx.nodes.isEmpty()) stringResource(R.string.import_nodes_endpoints) else "${ctx.nodes.size}")
                    InfoRow(stringResource(R.string.common_label_roles), ctx.roles.joinToString())
                    InfoRow(stringResource(R.string.common_label_cert_expires), certExpiry(ctx.certNotAfter))
                    preview.conflicts.firstOrNull { it.index == index }?.let { conflict ->
                        NameConflict(
                            name = ctx.name,
                            conflict = conflict,
                            choice = preview.choices.firstOrNull { it.index == index } ?: ImportChoice(index),
                            taken = index in preview.takenNames,
                            onRename = { onRename(index, it) },
                            onReplace = { onReplace(index, it) },
                        )
                    }
                }
            }
        }
        // The clusters already imported stay: say what this import does to them.
        if (adding) Text(stringResource(R.string.import_adds_cluster), style = MaterialTheme.typography.bodyMedium)
        preview.error?.let { Text(it, color = LocalStatusColors.current.bad, style = MaterialTheme.typography.bodyMedium) }
        MutedText(stringResource(R.string.import_stored_encrypted))
        Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            OutlinedButton(onClick = onCancel, modifier = Modifier.weight(1f)) { Text(stringResource(R.string.common_cancel)) }
            Button(onClick = onConfirm, enabled = preview.canImport, modifier = Modifier.weight(1f)) {
                Text(stringResource(R.string.import_import))
            }
        }
        Box(Modifier.height(8.dp))
    }
}

/**
 * A context named like a stored cluster: it is never overwritten silently. The user names
 * it (the suggested free name by default) or, for the same cluster, replaces the stored one.
 */
@Composable
internal fun NameConflict(
    name: String,
    conflict: ImportConflict,
    choice: ImportChoice,
    taken: Boolean,
    onRename: (String) -> Unit,
    onReplace: (Boolean) -> Unit,
) {
    Column(Modifier.padding(top = 12.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Text(
            stringResource(R.string.import_name_conflict, name),
            color = LocalStatusColors.current.warn,
            style = MaterialTheme.typography.bodyMedium,
        )
        OutlinedTextField(
            value = choice.name,
            onValueChange = onRename,
            enabled = !choice.replace,
            singleLine = true,
            label = { Text(stringResource(R.string.import_name_as)) },
            placeholder = { Text(conflict.suggested) },
            isError = taken,
            supportingText = if (taken) {
                { Text(stringResource(R.string.import_name_taken)) }
            } else {
                null
            },
            modifier = Modifier.fillMaxWidth(),
        )
        conflict.sameAs?.let { same ->
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    stringResource(R.string.import_name_replace, same),
                    style = MaterialTheme.typography.bodyMedium,
                    modifier = Modifier.weight(1f),
                )
                Switch(checked = choice.replace, onCheckedChange = onReplace)
            }
        }
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
