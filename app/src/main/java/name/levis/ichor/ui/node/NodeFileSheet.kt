package name.levis.ichor.ui.node

import android.content.Context
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import java.io.File
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import name.levis.ichor.R
import name.levis.ichor.model.NodeFileText
import name.levis.ichor.model.NodePaths
import name.levis.ichor.ui.UiState
import name.levis.ichor.TalosApp
import name.levis.ichor.ui.components.LoadFailureRow
import name.levis.ichor.ui.components.MonoScreen
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.ScalableMonoText
import name.levis.ichor.ui.components.copyToClipboard
import name.levis.ichor.ui.components.monoTextActions
import name.levis.ichor.ui.components.monoTextStyle
import name.levis.ichor.ui.components.shareFile
import name.levis.ichor.ui.design.IchorAlertDialog
import name.levis.ichor.ui.design.IchorOutlinedButton
import name.levis.ichor.ui.design.IchorTextButton
import name.levis.ichor.ui.uiStateOf
import name.levis.ichor.util.formatBytes

/** One file of the node: its text in pinch-to-resize monospace, with Copy and Share. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun NodeFileSheet(node: String, path: String, onDismiss: () -> Unit) {
    val talos = (LocalContext.current.applicationContext as TalosApp).talosRepository
    var attempt by remember { mutableIntStateOf(0) }
    val state by produceState<UiState<NodeFileText>>(UiState.Loading, node, path, attempt) {
        value = UiState.Loading
        value = uiStateOf { talos.nodeFileRead(node, path) }
    }
    ModalBottomSheet(onDismissRequest = onDismiss, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)) {
        Text(
            path.substringAfterLast('/').ifEmpty { path },
            style = MaterialTheme.typography.titleMedium,
            modifier = Modifier.padding(horizontal = 16.dp),
        )
        MutedText(path, Modifier.padding(horizontal = 16.dp, vertical = 2.dp))
        when (val s = state) {
            UiState.Loading -> CircularProgressIndicator(Modifier.align(Alignment.CenterHorizontally).padding(32.dp))
            is UiState.Failed -> LoadFailureRow(s.message, { attempt++ }, Modifier.padding(16.dp))
            is UiState.Loaded -> FileContent(s.data)
        }
    }
}

@Composable
private fun FileContent(file: NodeFileText) {
    val notes = listOfNotNull(
        stringResource(R.string.node_file_size, formatBytes(file.size)),
        stringResource(R.string.node_file_truncated).takeIf { file.truncated },
        stringResource(R.string.node_file_redacted).takeIf { file.redacted },
    )
    MutedText(notes.joinToString(" · "), Modifier.padding(horizontal = 16.dp))
    when {
        file.binary -> MutedText(stringResource(R.string.node_file_binary), Modifier.padding(16.dp))
        file.text.isEmpty() -> MutedText(stringResource(R.string.node_file_empty), Modifier.padding(16.dp))
        else -> {
            FileActions(file)
            val lines = remember(file.text) { file.text.trimEnd('\n').lines() }
            ScalableMonoText(MonoScreen.FILES) {
                SelectionContainer {
                    LazyColumn(contentPadding = PaddingValues(start = 16.dp, end = 16.dp, bottom = 32.dp)) {
                        itemsIndexed(lines) { _, line ->
                            Text(line, style = monoTextStyle(), modifier = Modifier.fillMaxWidth().monoTextActions())
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun FileActions(file: NodeFileText) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    Row(Modifier.padding(horizontal = 16.dp, vertical = 8.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        IchorOutlinedButton(onClick = { copyToClipboard(context, file.path, file.text, sensitive = file.redacted) }) {
            Text(stringResource(R.string.node_file_copy))
        }
        IchorOutlinedButton(onClick = {
            scope.launch {
                val shared = runCatching { withContext(Dispatchers.IO) { writeShareCopy(context, file) } }.getOrNull()
                if (shared != null) shareFile(context, shared, "text/plain", R.string.node_file_share_chooser)
            }
        }) {
            Text(stringResource(R.string.node_file_share))
        }
    }
}

/** A copy of the text under the cache's nodefiles/ (the only folder the provider shares for it). */
private fun writeShareCopy(context: Context, file: NodeFileText): File {
    val dir = File(context.cacheDir, "nodefiles").apply { mkdirs() }
    dir.listFiles()?.forEach { it.delete() }
    return File(dir, NodePaths.shareName(file.path)).apply { writeText(file.text) }
}

/** A typed absolute path, listed as a directory or read as a file. */
@Composable
internal fun GoToPathDialog(initial: String, onList: (String) -> Unit, onRead: (String) -> Unit, onDismiss: () -> Unit) {
    var input by rememberSaveable { mutableStateOf("$initial/") }
    val path = NodePaths.typed(input)
    IchorAlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.node_files_go_to)) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedTextField(
                    value = input,
                    onValueChange = { input = it },
                    label = { Text(stringResource(R.string.node_files_path_label)) },
                    singleLine = true,
                    isError = path == null,
                    textStyle = MaterialTheme.typography.bodyMedium.copy(fontFamily = FontFamily.Monospace),
                    modifier = Modifier.fillMaxWidth(),
                )
                if (path == null) MutedText(stringResource(R.string.node_files_path_invalid))
            }
        },
        confirmButton = {
            IchorTextButton(onClick = { path?.let { onList(it.trimEnd('/').ifEmpty { "/" }) } }, enabled = path != null) {
                Text(stringResource(R.string.node_files_go_list))
            }
        },
        dismissButton = {
            Row {
                IchorTextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) }
                IchorTextButton(onClick = { path?.let(onRead) }, enabled = path != null && !path.endsWith("/")) {
                    Text(stringResource(R.string.node_files_go_read))
                }
            }
        },
    )
}
