package name.levis.ichor.ui.diagnosis

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
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
import name.levis.ichor.R
import name.levis.ichor.data.AiToolCall
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.design.IchorTextButton
import name.levis.ichor.util.formatBytes

/** Everything the model asked for: each request, and on demand the exact text sent back. */
@Composable
fun AiToolRows(calls: List<AiToolCall>, unsupported: Boolean) {
    if (calls.isEmpty() && !unsupported) return
    Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
        if (calls.isNotEmpty()) {
            Text(stringResource(R.string.ai_tool_looked_at), style = MaterialTheme.typography.labelLarge)
        }
        calls.forEachIndexed { index, call -> AiToolRow(index, call) }
        if (unsupported) MutedText(stringResource(R.string.ai_tool_unsupported))
    }
}

@Composable
private fun AiToolRow(index: Int, call: AiToolCall) {
    var expanded by rememberSaveable(index) { mutableStateOf(false) }
    val summary = aiToolSummary(call)
    if (call.error != null) {
        MutedText(stringResource(R.string.ai_tool_refused, summary))
        MutedText(call.error)
        return
    }
    Row(verticalAlignment = Alignment.CenterVertically) {
        val size = if (call.done) " (${formatBytes(call.bytes)})" else ""
        Text(summary + size, style = MaterialTheme.typography.bodySmall, modifier = Modifier.weight(1f))
        if (call.result.isNotEmpty()) {
            IchorTextButton(onClick = { expanded = !expanded }) {
                Text(stringResource(if (expanded) R.string.ai_report_hide else R.string.ai_report_show))
            }
        }
    }
    if (expanded) {
        SelectionContainer {
            Text(call.result, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace)
        }
    }
}

@Composable
private fun aiToolSummary(call: AiToolCall): String = when (call.tool) {
    "pod_logs" -> stringResource(R.string.ai_tool_logs, call.args.pod)
    "object_events" -> stringResource(R.string.ai_tool_events, call.args.kind, call.args.name)
    "object_yaml" -> stringResource(R.string.ai_tool_yaml, call.args.kind, call.args.name)
    "service_logs" -> stringResource(R.string.ai_tool_service_logs, call.args.service, call.args.node)
    else -> stringResource(R.string.ai_tool_other, call.tool)
}
