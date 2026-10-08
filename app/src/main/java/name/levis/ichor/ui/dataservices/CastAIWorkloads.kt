package name.levis.ichor.ui.dataservices

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.material3.Card
import androidx.compose.material3.FilterChip
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.CastAIChange
import name.levis.ichor.model.CastAIMode
import name.levis.ichor.model.CastAIReason
import name.levis.ichor.model.CastAIRecommendation
import name.levis.ichor.model.CastAISection
import name.levis.ichor.model.CastAISectionKind
import name.levis.ichor.model.CastAIStatus
import name.levis.ichor.model.CastAIView
import name.levis.ichor.model.ServiceHealth
import name.levis.ichor.model.changeCounts
import name.levis.ichor.model.commonMode
import name.levis.ichor.model.formatMilliCores
import name.levis.ichor.model.sections
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.SearchField
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.formatBytes

/**
 * The Workload Autoscaler's recommendations, biggest changes first: what needs a look, then the
 * workloads whose requests grow (OOM and scheduling risk), then the savings. Each row draws the
 * requests before CAST AI against the recommended ones; a tap opens the containers.
 */
@Composable
fun CastAIWorkloads(status: CastAIStatus) {
    var view by rememberSaveable { mutableStateOf(CastAIView.OVERVIEW) }
    var query by rememberSaveable { mutableStateOf("") }
    var selected by rememberSaveable { mutableStateOf<String?>(null) }
    val sections = remember(status, view, query) { status.sections(view, query) }
    val growCount = remember(status) { status.recommendations.count { it.change == CastAIChange.GROW } }
    val listState = rememberLazyListState()

    LazyColumn(Modifier.fillMaxSize(), state = listState, contentPadding = PaddingValues(bottom = 16.dp)) {
        if (status.error.isNotEmpty()) item { InlineError(stringResource(R.string.data_services_unreadable, status.error), Modifier.padding(16.dp)) }
        if (status.recommendations.isEmpty()) {
            item { EmptyText(stringResource(R.string.castai_empty)) }
            return@LazyColumn
        }
        item { WorkloadsSummary(status) }
        item {
            Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                FilterChip(view == CastAIView.OVERVIEW, { view = CastAIView.OVERVIEW }, { Text(stringResource(R.string.castai_view_overview)) })
                FilterChip(view == CastAIView.GROWS, { view = CastAIView.GROWS }, { Text(stringResource(R.string.castai_view_grows, growCount)) })
                FilterChip(view == CastAIView.NAMESPACES, { view = CastAIView.NAMESPACES }, { Text(stringResource(R.string.castai_view_namespaces)) })
            }
        }
        item {
            SearchField(query, { query = it }, stringResource(R.string.castai_filter_hint), Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp))
        }
        if (sections.isEmpty()) item { EmptyText(stringResource(R.string.castai_no_match)) }
        sections.forEach { section -> section(section, onShowAll = { view = CastAIView.GROWS }, onOpen = { selected = it.namespace + "/" + it.name }) }
    }

    status.recommendations.firstOrNull { it.namespace + "/" + it.name == selected }?.let { rec ->
        CastAIWorkloadSheet(rec, onDismiss = { selected = null })
    }
}

private fun LazyListScope.section(section: CastAISection, onShowAll: () -> Unit, onOpen: (CastAIRecommendation) -> Unit) {
    item(key = "h:" + section.kind + section.namespace) { SectionHeader(section) }
    items(section.rows, key = { "r:" + section.kind + section.namespace + "/" + it.namespace + "/" + it.name }) { rec ->
        WorkloadRow(rec, Modifier.clickable { onOpen(rec) })
        HorizontalDivider(color = MaterialTheme.colorScheme.surfaceContainerHigh)
    }
    if (section.hidden > 0) item(key = "m:" + section.kind) {
        TextButton(onClick = onShowAll, modifier = Modifier.padding(horizontal = 8.dp)) {
            Text(stringResource(R.string.castai_show_all_grows, section.rows.size + section.hidden))
        }
    }
}

@Composable
private fun SectionHeader(section: CastAISection) {
    val colors = LocalStatusColors.current
    val (title, color) = when (section.kind) {
        CastAISectionKind.ATTENTION -> stringResource(R.string.castai_section_attention) to colors.bad
        CastAISectionKind.GROWS -> stringResource(R.string.castai_section_grows) to colors.warn
        CastAISectionKind.REDUCTIONS -> stringResource(R.string.castai_section_reductions) to colors.ok
        CastAISectionKind.OTHER -> stringResource(R.string.castai_section_other) to MaterialTheme.colorScheme.onSurfaceVariant
        CastAISectionKind.NAMESPACE -> section.namespace to MaterialTheme.colorScheme.onSurface
    }
    val note = if (section.kind == CastAISectionKind.NAMESPACE) {
        stringResource(R.string.castai_cpu_memory_delta, formatMilliCores(section.cpuDeltaMilli, signed = true), signedBytes(section.memoryDeltaBytes))
    } else {
        (section.rows.size + section.hidden).toString()
    }
    Row(Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, top = 18.dp, bottom = 4.dp), verticalAlignment = Alignment.Bottom) {
        Text(title, style = MaterialTheme.typography.titleSmall, color = color, modifier = Modifier.weight(1f))
        Text(note, style = MaterialTheme.typography.labelMedium, fontFamily = FontFamily.Monospace, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

/** What CAST AI changes across the cluster, which way each workload goes and how it is applied. */
@Composable
private fun WorkloadsSummary(status: CastAIStatus) {
    val colors = LocalStatusColors.current
    val counts = remember(status) { status.changeCounts() }
    Card(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp)) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(14.dp)) {
            Text(stringResource(R.string.castai_summary_title), style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.onSurfaceVariant)
            if (status.compared > 0) {
                Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                    MetricTile(
                        stringResource(R.string.castai_cpu_requests), formatMilliCores(status.cpuDeltaMilli, signed = true),
                        stringResource(R.string.castai_cores), deltaColor(status.cpuDeltaMilli), Modifier.weight(1f),
                    )
                    MetricTile(
                        stringResource(R.string.castai_memory_requests), signedBytes(status.memoryDeltaBytes),
                        stringResource(R.string.castai_per_pod), deltaColor(status.memoryDeltaBytes), Modifier.weight(1f),
                    )
                }
            }
            SplitBar(
                listOf(
                    SplitPart(counts.shrink, colors.ok, stringResource(R.string.castai_count_shrink, counts.shrink)),
                    SplitPart(counts.grow, colors.warn, stringResource(R.string.castai_count_grow, counts.grow)),
                    SplitPart(counts.same, colors.muted, stringResource(R.string.castai_count_same, counts.same)),
                    SplitPart(counts.unknown, MaterialTheme.colorScheme.surfaceContainerHighest, stringResource(R.string.castai_count_unknown, counts.unknown)),
                ),
            )
            status.commonMode()?.takeIf { it.first != CastAIMode.UNKNOWN }?.let { (mode, count) ->
                Text(
                    stringResource(R.string.castai_common_mode, castAIModeText(mode).orEmpty(), count.toString(), status.recommendations.size.toString()),
                    style = MaterialTheme.typography.labelMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }
}

/** Name, the deltas, a bar per resource, and why it needs a look if it does. */
@Composable
private fun WorkloadRow(rec: CastAIRecommendation, modifier: Modifier = Modifier) {
    val colors = LocalStatusColors.current
    Column(modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 10.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
        Row(verticalAlignment = Alignment.Top) {
            Column(Modifier.weight(1f)) {
                Text(rec.workload.ifEmpty { rec.name }, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis)
                Text(listOf(rec.namespace, rec.kind).filter { it.isNotEmpty() }.joinToString(" · "), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            if (rec.change != CastAIChange.UNKNOWN) {
                Column(horizontalAlignment = Alignment.End) {
                    Text(formatMilliCores(rec.cpuDeltaMilli, signed = true), style = MaterialTheme.typography.labelMedium, fontFamily = FontFamily.Monospace, color = deltaColor(rec.cpuDeltaMilli))
                    Text(signedBytes(rec.memoryDeltaBytes), style = MaterialTheme.typography.labelMedium, fontFamily = FontFamily.Monospace, color = deltaColor(rec.memoryDeltaBytes))
                }
            }
        }
        ResourceChangeRow(
            stringResource(R.string.castai_cpu), rec.originalCpuMilli.toDouble(), rec.cpuMilli.toDouble(),
            castAIChangeText(formatMilliCores(rec.originalCpuMilli), formatMilliCores(rec.cpuMilli)),
        )
        ResourceChangeRow(
            stringResource(R.string.castai_memory), rec.originalMemoryBytes.toDouble(), rec.memoryBytes.toDouble(),
            castAIChangeText(formatBytes(rec.originalMemoryBytes), formatBytes(rec.memoryBytes)),
        )
        if (rec.serviceHealth.needsAttention) {
            val reasons = rec.reasonList.map { castAIReasonText(it) }
            Text(
                (reasons + listOfNotNull(rec.message.takeIf { it.isNotEmpty() })).joinToString(" · "),
                style = MaterialTheme.typography.labelSmall,
                color = if (rec.serviceHealth == ServiceHealth.CRITICAL) colors.bad else colors.warn,
            )
        }
        if (rec.nearMemoryLimit) {
            Text(stringResource(R.string.castai_near_limit, rec.memoryLimitPercent.toString()), style = MaterialTheme.typography.labelSmall, color = colors.warn)
        }
    }
}

/** "500m → 120m", or the one value when it does not change. */
fun castAIChangeText(before: String, after: String): String = if (before == after) after else "$before → $after"

@Composable
fun castAIModeText(mode: CastAIMode): String? = when (mode) {
    CastAIMode.IMMEDIATE -> stringResource(R.string.castai_mode_immediate)
    CastAIMode.DEFERRED -> stringResource(R.string.castai_mode_deferred)
    CastAIMode.UNKNOWN -> null
}

@Composable
fun castAIReasonText(reason: CastAIReason): String = when (reason) {
    CastAIReason.VPA -> stringResource(R.string.castai_reason_vpa)
    CastAIReason.HPA -> stringResource(R.string.castai_reason_hpa)
    CastAIReason.READ_ONLY -> stringResource(R.string.castai_reason_read_only)
}
