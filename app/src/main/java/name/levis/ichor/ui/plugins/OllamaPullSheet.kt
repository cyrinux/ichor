package name.levis.ichor.ui.plugins

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardCapitalization
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import name.levis.ichor.R
import name.levis.ichor.model.OLLAMA_LIBRARY_URL
import name.levis.ichor.model.OLLAMA_SERVERS_CARD
import name.levis.ichor.model.OllamaPullState
import name.levis.ichor.model.PluginRow
import name.levis.ichor.model.isOllamaModelName
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.design.IchorAlertDialog
import name.levis.ichor.ui.design.IchorButton
import name.levis.ichor.ui.design.IchorOutlinedButton
import name.levis.ichor.ui.design.IchorTextButton
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.formatBytes

/**
 * "Pull a model…" under an Ollama server row (card [OLLAMA_SERVERS_CARD], whose rows open the
 * server's workload: its namespace and name are the server's); null for any other row.
 */
fun ollamaRowExtra(cardId: String, row: PluginRow, pulls: OllamaPulls): (@Composable () -> Unit)? {
    val ref = row.ref?.takeIf { cardId == OLLAMA_SERVERS_CARD && it.namespace.isNotEmpty() && it.name.isNotEmpty() } ?: return null
    return { OllamaPullButton(ref.namespace, ref.name, pulls) }
}

@Composable
private fun OllamaPullButton(namespace: String, server: String, pulls: OllamaPulls) {
    val pull by pulls.pull.collectAsStateWithLifecycle()
    var asking by rememberSaveable(namespace, server) { mutableStateOf(false) }
    IchorOutlinedButton(onClick = { asking = true }, enabled = pull?.running != true) { Text(stringResource(R.string.ollama_pull_action)) }
    if (asking) {
        OllamaModelPrompt(server, onDismiss = { asking = false }, onPull = { model ->
            asking = false
            pulls.start(namespace, server, model)
        })
    }
    pull?.takeIf { it.isFor(namespace, server) }?.let { OllamaPullSheet(it, onCancel = pulls::cancel, onClose = pulls::dismiss) }
}

/** The model's name, as the library lists it (name:tag), with a link to the library. */
@Composable
private fun OllamaModelPrompt(server: String, onDismiss: () -> Unit, onPull: (String) -> Unit) {
    var name by rememberSaveable { mutableStateOf("") }
    val model = name.trim()
    val valid = isOllamaModelName(model)
    val uri = LocalUriHandler.current
    IchorAlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.ollama_pull_title, server)) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedTextField(
                    value = name,
                    onValueChange = { name = it },
                    label = { Text(stringResource(R.string.ollama_pull_model_label)) },
                    placeholder = { Text(stringResource(R.string.ollama_pull_model_hint)) },
                    singleLine = true,
                    isError = model.isNotEmpty() && !valid,
                    keyboardOptions = KeyboardOptions(
                        capitalization = KeyboardCapitalization.None,
                        autoCorrectEnabled = false,
                        keyboardType = KeyboardType.Uri,
                        imeAction = ImeAction.Go,
                    ),
                    modifier = Modifier.fillMaxWidth(),
                )
                IchorTextButton(onClick = { runCatching { uri.openUri(OLLAMA_LIBRARY_URL) } }) { Text(stringResource(R.string.ollama_pull_library)) }
                MutedText(stringResource(R.string.ollama_pull_background_hint))
            }
        },
        confirmButton = { IchorButton(onClick = { onPull(model) }, enabled = valid) { Text(stringResource(R.string.ollama_pull_start)) } },
        dismissButton = { IchorTextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}

/** The pull's step and bar while it runs, then its outcome; Cancel stops the download. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun OllamaPullSheet(pull: OllamaPullState, onCancel: () -> Unit, onClose: () -> Unit) {
    ModalBottomSheet(
        onDismissRequest = { if (pull.running) onCancel() else onClose() },
        sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true),
    ) {
        Column(
            Modifier.fillMaxWidth().padding(horizontal = 20.dp).padding(bottom = 16.dp).navigationBarsPadding(),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            Text(pull.model, style = MaterialTheme.typography.titleLarge, fontFamily = FontFamily.Monospace)
            MutedText("${pull.namespace}/${pull.server}")
            OllamaPullBody(pull)
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.End) {
                if (pull.running) {
                    IchorOutlinedButton(onClick = onCancel) { Text(stringResource(R.string.ollama_pull_cancel)) }
                } else {
                    IchorButton(onClick = onClose) { Text(stringResource(R.string.common_close)) }
                }
            }
        }
    }
}

@Composable
private fun OllamaPullBody(pull: OllamaPullState) {
    val progress = pull.progress
    when {
        pull.error != null -> InlineError(pull.error)
        pull.done -> Text(stringResource(R.string.ollama_pull_done), color = LocalStatusColors.current.ok)
        else -> {
            val fraction = progress.fraction
            if (fraction == null) {
                LinearProgressIndicator(Modifier.fillMaxWidth())
            } else {
                LinearProgressIndicator(progress = { fraction }, modifier = Modifier.fillMaxWidth())
            }
            val status = progress.status.ifEmpty { stringResource(R.string.ollama_pull_starting) }
            Text(
                if (progress.total > 0) {
                    stringResource(R.string.ollama_pull_progress, status, formatBytes(progress.completed), formatBytes(progress.total))
                } else {
                    status
                },
                style = MaterialTheme.typography.bodyMedium,
            )
            MutedText(stringResource(R.string.ollama_pull_background_hint))
        }
    }
}
