package name.levis.ichor.ui.node

import androidx.annotation.StringRes
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.fillMaxWidth
import name.levis.ichor.ui.design.IchorAlertDialog
import name.levis.ichor.ui.design.IchorFilterChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import name.levis.ichor.ui.design.IchorTextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.NodeTaintEdit
import name.levis.ichor.model.TAINT_EFFECTS

/**
 * Adds a taint ([initial] null) or edits [initial]: its key, value and effect. The API server's
 * dry run judges the syntax at review time; here only an empty key or effect is refused.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun TaintEditDialog(initial: NodeTaintEdit?, onSave: (NodeTaintEdit) -> Unit, onDismiss: () -> Unit) {
    var key by remember { mutableStateOf(initial?.key.orEmpty()) }
    var value by remember { mutableStateOf(initial?.value.orEmpty()) }
    var effect by remember { mutableStateOf(initial?.effect ?: TAINT_EFFECTS.first()) }
    val valid = key.isNotBlank() && effect in TAINT_EFFECTS
    IchorAlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(if (initial == null) R.string.node_meta_add_taint else R.string.node_meta_edit)) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                MetaField(key, { key = it }, R.string.node_meta_key)
                MetaField(value, { value = it }, R.string.node_meta_value)
                Text(stringResource(R.string.node_meta_effect), style = MaterialTheme.typography.labelLarge)
                FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    // Kubernetes' own names, never translated.
                    TAINT_EFFECTS.forEach { name ->
                        IchorFilterChip(selected = effect == name, onClick = { effect = name }, label = { Text(name) })
                    }
                }
            }
        },
        confirmButton = {
            IchorTextButton(onClick = { onSave(NodeTaintEdit(key.trim(), value.trim(), effect)) }, enabled = valid) {
                Text(stringResource(R.string.common_ok))
            }
        },
        dismissButton = { IchorTextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}

/** Adds a label ([initialKey] null) or edits the label [initialKey]. */
@Composable
internal fun LabelEditDialog(
    initialKey: String?,
    initialValue: String,
    onSave: (key: String, value: String) -> Unit,
    onDismiss: () -> Unit,
) {
    var key by remember { mutableStateOf(initialKey.orEmpty()) }
    var value by remember { mutableStateOf(initialValue) }
    IchorAlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(if (initialKey == null) R.string.node_meta_add_label else R.string.node_meta_edit)) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                MetaField(key, { key = it }, R.string.node_meta_key)
                MetaField(value, { value = it }, R.string.node_meta_value)
            }
        },
        confirmButton = {
            IchorTextButton(onClick = { onSave(key.trim(), value.trim()) }, enabled = key.isNotBlank()) {
                Text(stringResource(R.string.common_ok))
            }
        },
        dismissButton = { IchorTextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}

@Composable
private fun MetaField(value: String, onChange: (String) -> Unit, @StringRes label: Int) {
    OutlinedTextField(
        value = value,
        onValueChange = onChange,
        label = { Text(stringResource(label)) },
        singleLine = true,
        textStyle = MaterialTheme.typography.bodyMedium.copy(fontFamily = FontFamily.Monospace),
        modifier = Modifier.fillMaxWidth(),
    )
}
