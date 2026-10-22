package name.levis.ichor.ui.runbooks

import android.content.Context
import android.net.Uri
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.annotation.StringRes
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalClipboard
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.data.RunbookRepository
import name.levis.ichor.model.RUNBOOK_MAX_BYTES
import name.levis.ichor.model.RunbookPlan
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.userMessage
import name.levis.ichor.util.readBounded

private enum class RunbookSource(@StringRes val label: Int) {
    PASTE(R.string.plugins_source_paste),
    FILE(R.string.plugins_source_file),
    URL(R.string.plugins_source_url),
}

/**
 * Imports a runbook: pasted, picked or downloaded from an https URL; Show plan has the core
 * plan it against the cluster on screen, storing nothing and running nothing. The consent
 * step shows that plan; only Keep stores it. A runbook is never run on import.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun RunbookImportSheet(repo: RunbookRepository, scope: String, onImported: () -> Unit, onDismiss: () -> Unit) {
    val coroutines = rememberCoroutineScope()
    var planned by remember { mutableStateOf<Pair<String, RunbookPlan>?>(null) }
    var keeping by remember { mutableStateOf(false) }
    var failure by remember { mutableStateOf<String?>(null) }

    ModalBottomSheet(onDismissRequest = { if (!keeping) onDismiss() }) {
        Column(
            Modifier.verticalScroll(rememberScrollState()).padding(horizontal = 20.dp).padding(bottom = 16.dp).navigationBarsPadding(),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            val current = planned
            if (current == null) {
                ImportStep(repo, onPlanned = { yaml, plan -> planned = yaml to plan })
            } else {
                Text(stringResource(R.string.runbook_consent_title, current.second.name), style = MaterialTheme.typography.titleLarge)
                if (current.second.description.isNotEmpty()) MutedText(current.second.description)
                RunbookPlanSteps(current.second, continueIds = emptySet(), onToggleContinue = null)
                MutedText(stringResource(R.string.runbook_consent_note))
                failure?.let {
                    Text(it, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall, modifier = Modifier.semantics { liveRegion = LiveRegionMode.Polite })
                }
                Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp, Alignment.End)) {
                    TextButton(onClick = onDismiss, enabled = !keeping) { Text(stringResource(R.string.common_cancel)) }
                    Button(
                        onClick = {
                            keeping = true
                            failure = null
                            coroutines.launch {
                                try {
                                    repo.import(scope, current.first)
                                    onImported()
                                    onDismiss()
                                } catch (e: CancellationException) {
                                    throw e
                                } catch (e: Exception) {
                                    failure = e.userMessage()
                                }
                                keeping = false
                            }
                        },
                        enabled = !keeping,
                    ) { Text(stringResource(R.string.runbook_keep)) }
                }
            }
        }
    }
}

@Composable
private fun ImportStep(repo: RunbookRepository, onPlanned: (yaml: String, plan: RunbookPlan) -> Unit) {
    val context = LocalContext.current
    val clipboard = LocalClipboard.current
    val scope = rememberCoroutineScope()
    var source by remember { mutableStateOf(RunbookSource.PASTE) }
    var yaml by remember { mutableStateOf("") }
    var url by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }

    fun run(block: suspend () -> Unit) {
        busy = true
        error = null
        scope.launch {
            try {
                block()
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                error = e.userMessage()
            }
            busy = false
        }
    }

    fun plan(text: String) = run { onPlanned(text, repo.plan(text)) }

    val picker = rememberLauncherForActivityResult(ActivityResultContracts.OpenDocument()) { uri: Uri? ->
        if (uri != null) {
            readRunbookFile(context, uri).fold(
                onSuccess = {
                    yaml = it
                    plan(it)
                },
                onFailure = { error = if (it is IllegalArgumentException) context.getString(R.string.runbook_file_too_large) else it.userMessage() },
            )
        }
    }

    Text(stringResource(R.string.runbooks_import), style = MaterialTheme.typography.titleLarge)
    SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth()) {
        RunbookSource.entries.forEachIndexed { index, s ->
            SegmentedButton(
                selected = s == source,
                onClick = {
                    source = s
                    error = null
                },
                shape = SegmentedButtonDefaults.itemShape(index, RunbookSource.entries.size),
            ) { Text(stringResource(s.label)) }
        }
    }
    when (source) {
        RunbookSource.PASTE -> {
            OutlinedTextField(
                value = yaml,
                onValueChange = { if (it.length <= RUNBOOK_MAX_BYTES) yaml = it },
                label = { Text(stringResource(R.string.runbook_field_yaml)) },
                textStyle = MaterialTheme.typography.bodySmall.copy(fontFamily = FontFamily.Monospace),
                minLines = 6,
                modifier = Modifier.fillMaxWidth().heightIn(max = 320.dp),
            )
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedButton(onClick = {
                    scope.launch {
                        clipboard.getClipEntry()?.clipData?.takeIf { it.itemCount > 0 }
                            ?.getItemAt(0)?.coerceToText(context)?.toString()?.let { yaml = it }
                    }
                }) { Text(stringResource(R.string.plugins_paste_clipboard)) }
                Button(onClick = { plan(yaml) }, enabled = yaml.isNotBlank() && !busy) { Text(stringResource(R.string.runbook_show_plan)) }
            }
        }
        RunbookSource.FILE ->
            Button(onClick = { picker.launch(arrayOf("*/*")) }, enabled = !busy, modifier = Modifier.fillMaxWidth()) {
                Text(stringResource(R.string.plugins_pick_file))
            }
        RunbookSource.URL -> {
            OutlinedTextField(
                value = url,
                onValueChange = { url = it },
                label = { Text(stringResource(R.string.runbook_field_url)) },
                singleLine = true,
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri, autoCorrectEnabled = false),
                modifier = Modifier.fillMaxWidth(),
            )
            Button(
                onClick = {
                    run {
                        val text = repo.download(url.trim())
                        yaml = text
                        onPlanned(text, repo.plan(text))
                    }
                },
                enabled = url.isNotBlank() && !busy,
                modifier = Modifier.fillMaxWidth(),
            ) { Text(stringResource(R.string.runbook_download)) }
        }
    }
    if (busy) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp)
            MutedText(stringResource(R.string.runbook_planning))
        }
    }
    error?.let {
        Column(Modifier.semantics { liveRegion = LiveRegionMode.Polite }) {
            Text(stringResource(R.string.runbook_import_failed), color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodyMedium)
            Text(it, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall.copy(fontFamily = FontFamily.Monospace))
        }
    }
}

/** The picked file's text, at most [RUNBOOK_MAX_BYTES] (more is an IllegalArgumentException). */
private fun readRunbookFile(context: Context, uri: Uri): Result<String> = runCatching {
    context.contentResolver.openInputStream(uri)?.use { readBounded(it, RUNBOOK_MAX_BYTES).decodeToString() }
        ?: error("cannot open the file")
}
