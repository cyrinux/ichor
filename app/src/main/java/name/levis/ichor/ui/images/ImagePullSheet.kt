package name.levis.ichor.ui.images

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import name.levis.ichor.R
import name.levis.ichor.data.ImagePullManager
import name.levis.ichor.data.ImagePullRunState
import name.levis.ichor.model.ImagePullNamespace
import name.levis.ichor.model.ImagePullNode
import name.levis.ichor.model.ImagePullState
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.theme.LocalStatusColors

/** The followed image pull: each node's state, Stop while it runs, Close once it ended. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ImagePullSheet(manager: ImagePullManager, onDismiss: () -> Unit) {
    val run by manager.current.collectAsStateWithLifecycle()
    ModalBottomSheet(onDismissRequest = onDismiss) {
        run?.let { ImagePullContent(it, onStop = manager::stop, onClose = { manager.dismiss(); onDismiss() }) }
    }
}

@Composable
private fun ImagePullContent(run: ImagePullRunState, onStop: () -> Unit, onClose: () -> Unit) {
    val colors = LocalStatusColors.current
    val progress = run.progress
    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp).padding(bottom = 24.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Text(stringResource(R.string.image_pull_title), style = MaterialTheme.typography.titleMedium)
        SelectionContainer { Text(run.image, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace) }
        MutedText(stringResource(namespaceLabel(run.namespace)))
        if (progress.total > 0) {
            LinearProgressIndicator(progress = { progress.done.toFloat() / progress.total }, modifier = Modifier.fillMaxWidth())
            Text(stringResource(R.string.image_pull_progress, progress.done, progress.total), style = MaterialTheme.typography.bodyMedium)
        } else if (run.running) {
            LinearProgressIndicator(Modifier.fillMaxWidth())
        }
        when {
            run.stopping -> Text(stringResource(R.string.image_pull_stopped), color = colors.warn, style = MaterialTheme.typography.bodyMedium)
            run.error != null -> Text(run.error, color = colors.bad, style = MaterialTheme.typography.bodyMedium)
            run.finished -> Text(stringResource(R.string.image_pull_notification_done), color = colors.ok, style = MaterialTheme.typography.bodyMedium)
        }
        HorizontalDivider()
        LazyColumn(Modifier.fillMaxWidth().weight(1f, fill = false)) {
            items(progress.nodes, key = { it.node }) { node ->
                PullNodeRow(node)
                HorizontalDivider()
            }
        }
        if (run.running) {
            OutlinedButton(onClick = onStop, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.image_pull_stop)) }
        } else {
            Button(onClick = onClose, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.common_close)) }
        }
    }
}

@Composable
private fun PullNodeRow(node: ImagePullNode) {
    val colors = LocalStatusColors.current
    val state = node.pullState
    val color = when (state) {
        ImagePullState.DONE -> colors.ok
        ImagePullState.FAILED -> colors.bad
        ImagePullState.PULLING -> MaterialTheme.colorScheme.primary
        ImagePullState.PENDING -> colors.muted
    }
    Column(Modifier.fillMaxWidth().padding(vertical = 6.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(node.label, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, modifier = Modifier.weight(1f))
            Text(stringResource(stateLabel(state)), style = MaterialTheme.typography.labelMedium, color = color)
        }
        if (node.error.isNotEmpty()) {
            Text(node.error, style = MaterialTheme.typography.bodySmall, color = colors.bad)
        }
    }
}

private fun stateLabel(state: ImagePullState): Int = when (state) {
    ImagePullState.PENDING -> R.string.image_pull_state_pending
    ImagePullState.PULLING -> R.string.image_pull_state_pulling
    ImagePullState.DONE -> R.string.image_pull_state_done
    ImagePullState.FAILED -> R.string.image_pull_state_failed
}

private fun namespaceLabel(namespace: ImagePullNamespace): Int = when (namespace) {
    ImagePullNamespace.SYSTEM -> R.string.image_pull_namespace_system
    ImagePullNamespace.CRI -> R.string.image_pull_namespace_cri
}

/**
 * Asks which image to pull on every node: a Kubernetes image by default (the Images screen),
 * or a Talos system image (an installer).
 */
@Composable
fun ImagePullDialog(onPull: (String, ImagePullNamespace) -> Unit, onDismiss: () -> Unit) {
    var image by rememberSaveable { mutableStateOf("") }
    var namespace by rememberSaveable { mutableStateOf(ImagePullNamespace.CRI) }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.image_pull_menu)) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedTextField(
                    value = image,
                    onValueChange = { image = it },
                    label = { Text(stringResource(R.string.image_pull_image_label)) },
                    singleLine = true,
                    textStyle = MaterialTheme.typography.bodyMedium.copy(fontFamily = FontFamily.Monospace),
                    modifier = Modifier.fillMaxWidth(),
                )
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    ImagePullNamespace.entries.forEach { ns ->
                        FilterChip(selected = namespace == ns, onClick = { namespace = ns }, label = { Text(stringResource(namespaceLabel(ns))) })
                    }
                }
                MutedText(stringResource(R.string.image_pull_namespace_hint))
            }
        },
        confirmButton = {
            TextButton(onClick = { onPull(image.trim(), namespace) }, enabled = image.isNotBlank()) {
                Text(stringResource(R.string.image_pull_start))
            }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}
