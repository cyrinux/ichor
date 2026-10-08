package name.levis.ichor.ui.metrics

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.AutoAwesome
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.model.PromPanel
import name.levis.ichor.model.PromResult
import name.levis.ichor.ui.userMessage

/**
 * Adds (blank [panel] id) or edits a panel, from a preset or plain PromQL, with a preview run.
 * The draft lives in the caller ([onChange]), so the panel assistant can fill it in; [onAskAi]
 * opens the assistant, null when AI is off.
 */
@Composable
fun PanelEditorDialog(
    panel: PromPanel,
    onChange: (PromPanel) -> Unit,
    presets: List<PromPanel>,
    onPreview: suspend (PromPanel) -> Result<PromResult>,
    onSave: (PromPanel) -> Unit,
    onDismiss: () -> Unit,
    onAskAi: (() -> Unit)? = null,
) {
    var preview by remember { mutableStateOf<Result<PromResult>?>(null) }
    var running by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()

    // A preview is about one query: another one (typed, picked or proposed) drops it.
    LaunchedEffect(panel.query) { preview = null }

    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(if (panel.id.isBlank()) R.string.metrics_add_panel else R.string.metrics_edit_panel)) },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                if (panel.id.isBlank() && presets.isNotEmpty()) {
                    PresetPicker(presets) { preset -> onChange(preset.copy(id = "")) }
                }
                OutlinedTextField(
                    panel.title, { onChange(panel.copy(title = it)) },
                    label = { Text(stringResource(R.string.metrics_panel_title)) },
                    singleLine = true, modifier = Modifier.fillMaxWidth(),
                )
                OutlinedTextField(
                    panel.query, { onChange(panel.copy(query = it)) },
                    label = { Text(stringResource(R.string.metrics_panel_query)) },
                    textStyle = TextStyle(fontFamily = FontFamily.Monospace),
                    minLines = 3, modifier = Modifier.fillMaxWidth(),
                )
                UnitPicker(panel.unit) { onChange(panel.copy(unit = it)) }
                OutlinedTextField(
                    panel.legend, { onChange(panel.copy(legend = it)) },
                    label = { Text(stringResource(R.string.metrics_panel_legend)) },
                    supportingText = { Text(stringResource(R.string.metrics_panel_legend_hint)) },
                    singleLine = true, modifier = Modifier.fillMaxWidth(),
                )
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    OutlinedButton(
                        enabled = panel.query.isNotBlank() && !running,
                        onClick = {
                            running = true
                            scope.launch {
                                preview = onPreview(panel)
                                running = false
                            }
                        },
                    ) { Text(stringResource(R.string.metrics_run)) }
                    if (onAskAi != null) {
                        OutlinedButton(onClick = onAskAi) {
                            Icon(Icons.Outlined.AutoAwesome, null, Modifier.size(18.dp))
                            Spacer(Modifier.width(6.dp))
                            Text(stringResource(R.string.metrics_ai_ask))
                        }
                    }
                }
                if (running) LinearProgressIndicator(Modifier.fillMaxWidth())
                preview?.fold(
                    onSuccess = { PromChart(panel, it) },
                    onFailure = { Text(it.userMessage(), color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall) },
                )
            }
        },
        confirmButton = {
            TextButton(
                enabled = panel.title.isNotBlank() && panel.query.isNotBlank(),
                onClick = { onSave(panel.copy(title = panel.title.trim(), query = panel.query.trim(), legend = panel.legend.trim())) },
            ) { Text(stringResource(R.string.metrics_save)) }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}

@Composable
private fun PresetPicker(presets: List<PromPanel>, onPick: (PromPanel) -> Unit) {
    var open by remember { mutableStateOf(false) }
    Box {
        OutlinedButton(onClick = { open = true }) { Text(stringResource(R.string.metrics_from_preset)) }
        DropdownMenu(expanded = open, onDismissRequest = { open = false }) {
            presets.forEach { p -> DropdownMenuItem(text = { Text(p.title) }, onClick = { open = false; onPick(p) }) }
        }
    }
}

@Composable
private fun UnitPicker(unit: String, onUnit: (String) -> Unit) {
    var open by remember { mutableStateOf(false) }
    Box {
        OutlinedButton(onClick = { open = true }) { Text(stringResource(R.string.metrics_panel_unit, unitLabel(unit))) }
        DropdownMenu(expanded = open, onDismissRequest = { open = false }) {
            METRICS_UNITS.forEach { u -> DropdownMenuItem(text = { Text(unitLabel(u)) }, onClick = { open = false; onUnit(u) }) }
        }
    }
}

@Composable
internal fun unitLabel(unit: String): String = stringResource(
    when (unit) {
        "percent" -> R.string.metrics_unit_percent
        "bytes" -> R.string.metrics_unit_bytes
        "cores" -> R.string.metrics_unit_cores
        "persec" -> R.string.metrics_unit_persec
        "count" -> R.string.metrics_unit_count
        else -> R.string.metrics_unit_none
    },
)
