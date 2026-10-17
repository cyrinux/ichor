package name.levis.ichor.ui.insights

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Button
import androidx.compose.material3.FilterChip
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.IncidentDocument
import name.levis.ichor.model.ServiceHealth
import name.levis.ichor.ui.components.InfoHint
import name.levis.ichor.ui.components.ResourceRow
import name.levis.ichor.ui.components.SectionHeader

/**
 * The incident recorder tab: a header (title, hint, the date range and dropped count), the
 * Start/Stop, Delete and metrics controls, then the entries as a newest-first timeline.
 */
@OptIn(ExperimentalLayoutApi::class)
fun LazyListScope.recorderPage(
    state: InsightsState,
    metrics: Boolean,
    onMetrics: (Boolean) -> Unit,
    onToggleRecording: () -> Unit,
    onDelete: () -> Unit,
    time: (Long) -> String,
) {
    val document = state.document
    item {
        val title = stringResource(R.string.insights_recorder)
        SectionHeader(
            title,
            subtitle = document?.let { recorderSummary(it, time) },
            trailing = { InfoHint(title, stringResource(R.string.insights_record_note)) },
        )
    }
    item {
        FlowRow(horizontalArrangement = Arrangement.spacedBy(12.dp), itemVerticalAlignment = Alignment.CenterVertically) {
            Button(onClick = onToggleRecording) {
                Text(stringResource(if (state.recording) R.string.insights_stop else R.string.insights_start))
            }
            TextButton(onClick = onDelete, enabled = !state.recording && document != null) { Text(stringResource(R.string.common_delete)) }
            if (document != null) {
                FilterChip(selected = metrics, onClick = { onMetrics(!metrics) }, label = { Text(stringResource(R.string.insights_show_metrics)) })
            }
        }
    }
    if (state.recording) item { LinearProgressIndicator(Modifier.fillMaxWidth()) }
    if (document == null) {
        item { Text(stringResource(R.string.insights_no_recording)) }
        return
    }
    val shown = document.entries.asReversed().filter { metrics || it.kind != "metrics" }
    items(incidentTimeline(shown, document.startedAt), key = { it.key }) { item ->
        when (item) {
            is TimelineItem.Single -> TimelineRow(item.row, time)
            is TimelineItem.HealthyRun -> HealthyRunRow(item, time)
        }
    }
}

@Composable
private fun recorderSummary(document: IncidentDocument, time: (Long) -> String): String {
    val range = "${time(document.startedAt)} — ${time(document.updatedAt)}"
    return if (document.dropped > 0) "$range · ${stringResource(R.string.insights_dropped, document.dropped)}" else range
}

@Composable
private fun TimelineRow(row: TimelineEntry, time: (Long) -> String, modifier: Modifier = Modifier) {
    var expanded by rememberSaveable(row.entry.id) { mutableStateOf(false) }
    val entry = row.entry
    val subtitle = listOf(evidenceKindLabel(entry.kind), evidenceStateLabel(entry), row.offset)
        .filter { it.isNotBlank() }
        .joinToString(" · ")
    ResourceRow(
        health = row.health,
        title = entry.subject.ifBlank { entry.node },
        subtitle = subtitle,
        modifier = modifier,
        onClick = { expanded = !expanded },
        expanded = expanded,
    ) {
        if (expanded) IncidentEvidenceBody(entry, time(entry.at))
    }
}

@Composable
private fun HealthyRunRow(run: TimelineItem.HealthyRun, time: (Long) -> String) {
    var expanded by rememberSaveable(run.key) { mutableStateOf(false) }
    val count = run.rows.size
    ResourceRow(
        health = ServiceHealth.OK,
        title = pluralStringResource(R.plurals.insights_healthy_run, count, count),
        subtitle = "${run.node} · ${run.rows.last().offset} – ${run.rows.first().offset}",
        mono = false,
        onClick = { expanded = !expanded },
        expanded = expanded,
    ) {
        if (expanded) {
            run.rows.forEach { row -> TimelineRow(row, time) }
        }
    }
}
