package name.levis.ichor.ui.kubebrowser

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.selection.selectableGroup
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.DeletePropagation
import name.levis.ichor.model.KubeDeletePreview
import name.levis.ichor.model.KubeObjectRef
import name.levis.ichor.model.KubePermission
import name.levis.ichor.model.confirmationMatches
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.KubeDenialNote
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * Confirms deleting [ref]: what the deletion would touch (protection, finalizers, the objects
 * it owns, from the preview), what happens to what it owns, and, for a cluster-scoped or
 * protected object, its name typed as for the risky node actions. A protected object is
 * deleted only through "Delete anyway" (force). [denial] (the credentials may not delete it)
 * disables the deletion and says why.
 */
@Composable
fun KubeObjectDeleteDialog(
    ref: KubeObjectRef,
    delete: ObjectDelete,
    denial: KubePermission?,
    onPropagation: (DeletePropagation) -> Unit,
    onConfirm: (force: Boolean) -> Unit,
    onRetry: () -> Unit,
    onDismiss: () -> Unit,
) {
    var typed by remember { mutableStateOf("") }
    val preview = (delete.preview as? UiState.Loaded)?.data
    val typedOk = preview != null && (!preview.needsTypedName || confirmationMatches(typed, ref.name))
    val enabled = typedOk && !delete.deleting && denial == null
    val colors = LocalStatusColors.current

    AlertDialog(
        onDismissRequest = { if (!delete.deleting) onDismiss() },
        title = { Text(stringResource(R.string.kb_delete_title, ref.name)) },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                when (val p = delete.preview) {
                    UiState.Loading -> Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                        CircularProgressIndicator(Modifier.padding(4.dp))
                        MutedText(stringResource(R.string.kb_delete_loading))
                    }
                    is UiState.Failed -> {
                        ErrorCard(p.message.asString())
                        TextButton(onClick = onRetry) { Text(stringResource(R.string.common_retry)) }
                    }
                    is UiState.Loaded -> DeletePreviewContent(p.data, delete.propagation, onPropagation)
                }
                if (preview?.needsTypedName == true) {
                    Text(stringResource(R.string.power_type_to_confirm, ref.name), style = MaterialTheme.typography.bodyMedium)
                    OutlinedTextField(
                        value = typed,
                        onValueChange = { typed = it },
                        singleLine = true,
                        textStyle = MaterialTheme.typography.bodyMedium.copy(fontFamily = FontFamily.Monospace),
                        modifier = Modifier.fillMaxWidth(),
                    )
                }
                delete.error?.let { ErrorCard(it.asString()) }
                KubeDenialNote(denial)
            }
        },
        confirmButton = {
            Row(verticalAlignment = Alignment.CenterVertically) {
                if (delete.deleting) CircularProgressIndicator(Modifier.padding(end = 8.dp))
                TextButton(onClick = { onConfirm(preview?.isProtected == true) }, enabled = enabled) {
                    Text(
                        stringResource(if (preview?.isProtected == true) R.string.kb_delete_anyway else R.string.kb_delete),
                        color = if (enabled) colors.bad else MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
        },
        dismissButton = { TextButton(onClick = onDismiss, enabled = !delete.deleting) { Text(stringResource(R.string.common_cancel)) } },
    )
}

@Composable
private fun DeletePreviewContent(p: KubeDeletePreview, propagation: DeletePropagation, onPropagation: (DeletePropagation) -> Unit) {
    val colors = LocalStatusColors.current
    if (p.isProtected) ErrorCard(stringResource(R.string.kb_delete_protected, p.reason))
    if (p.deleting) Text(stringResource(R.string.kb_delete_pending), color = colors.warn, style = MaterialTheme.typography.bodySmall)
    if (p.finalizers.isNotEmpty()) {
        Text(stringResource(R.string.kb_delete_finalizers), style = MaterialTheme.typography.bodySmall)
        p.finalizers.forEach { MonoLine(it) }
    }
    if (p.dependents.isNotEmpty()) {
        Text(stringResource(R.string.kb_delete_dependents, p.dependents.size + p.moreDependents), style = MaterialTheme.typography.bodySmall)
        p.dependents.forEach { MonoLine("${it.kind} ${it.name}") }
        if (p.moreDependents > 0) MutedText(stringResource(R.string.kb_delete_more, p.moreDependents))
    }
    Text(stringResource(R.string.kb_delete_propagation), style = MaterialTheme.typography.titleSmall)
    Column(Modifier.selectableGroup()) {
        DeletePropagation.entries.forEach { option ->
            PropagationOption(option, option == propagation) { onPropagation(option) }
        }
    }
}

@Composable
private fun MonoLine(text: String) {
    Text(text, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace, modifier = Modifier.padding(start = 8.dp))
}

@Composable
private fun PropagationOption(option: DeletePropagation, selected: Boolean, onClick: () -> Unit) {
    val (title, description) = when (option) {
        DeletePropagation.BACKGROUND -> R.string.kb_delete_background to R.string.kb_delete_background_desc
        DeletePropagation.FOREGROUND -> R.string.kb_delete_foreground to R.string.kb_delete_foreground_desc
        DeletePropagation.ORPHAN -> R.string.kb_delete_orphan to R.string.kb_delete_orphan_desc
    }
    Row(
        Modifier.fillMaxWidth().selectable(selected = selected, role = Role.RadioButton, onClick = onClick).padding(vertical = 4.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        RadioButton(selected = selected, onClick = null)
        Column(Modifier.padding(start = 8.dp)) {
            Text(stringResource(title), style = MaterialTheme.typography.bodyMedium)
            MutedText(stringResource(description))
        }
    }
}
