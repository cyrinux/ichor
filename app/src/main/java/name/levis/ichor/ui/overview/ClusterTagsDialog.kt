package name.levis.ichor.ui.overview

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.normalizeTags
import name.levis.ichor.model.tagSuggestions
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.design.IchorAlertDialog
import name.levis.ichor.ui.design.IchorAssistChip
import name.levis.ichor.ui.design.IchorTextButton

/**
 * The tags of the cluster [label], [current] so far, comma-separated in one field; a tap on a
 * tag of another cluster ([saved] minus the [own] fingerprints) adds it. Not destructive.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun ClusterTagsDialog(
    label: String,
    current: List<String>,
    saved: Map<String, List<String>>,
    own: List<String>,
    onSave: (String) -> Unit,
    onDismiss: () -> Unit,
) {
    var text by remember { mutableStateOf(current.joinToString(", ")) }
    IchorAlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.cluster_tags_title, label)) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                OutlinedTextField(
                    value = text,
                    onValueChange = { text = it },
                    label = { Text(stringResource(R.string.cluster_tags_label)) },
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth(),
                )
                Text(stringResource(R.string.cluster_tags_hint), style = MaterialTheme.typography.bodySmall)
                val suggestions = tagSuggestions(saved, own, text)
                if (suggestions.isNotEmpty()) {
                    MutedText(stringResource(R.string.cluster_tags_suggestions))
                    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        suggestions.forEach { tag ->
                            IchorAssistChip(
                                onClick = { text = (normalizeTags(text) + tag).joinToString(", ") },
                                label = { Text(tag) },
                            )
                        }
                    }
                }
            }
        },
        confirmButton = { IchorTextButton(onClick = { onSave(text) }) { Text(stringResource(R.string.common_ok)) } },
        dismissButton = { IchorTextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}
