package name.levis.ichor.ui.dataservices

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Card
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.CastAIContainer
import name.levis.ichor.model.CastAIMode
import name.levis.ichor.model.CastAIReason
import name.levis.ichor.model.CastAIRecommendation
import name.levis.ichor.model.CastAIStatus
import name.levis.ichor.model.formatMilliCores
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.SearchField
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.formatBytes

/**
 * CAST AI's Workload Autoscaler recommendations (problems first): per workload, the requests it
 * first saw against the ones it recommends, how it applies them, and why it cannot. A filter box
 * narrows by namespace or workload: a large cluster has hundreds.
 */
@Composable
fun CastAITab(status: CastAIStatus) {
    var query by rememberSaveable { mutableStateOf("") }
    val shown = remember(status, query) {
        val q = query.trim()
        if (q.isEmpty()) status.recommendations else status.recommendations.filter { it.label.contains(q, ignoreCase = true) }
    }

    LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 16.dp)) {
        if (status.error.isNotEmpty()) item { InlineError(stringResource(R.string.data_services_unreadable, status.error), Modifier.padding(16.dp)) }
        if (status.recommendations.isEmpty()) {
            item { EmptyText(stringResource(R.string.castai_empty)) }
            return@LazyColumn
        }
        item { TotalsCard(status) }
        item {
            SearchField(query, { query = it }, stringResource(R.string.castai_filter_hint), Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp))
        }
        if (shown.isEmpty()) item { EmptyText(stringResource(R.string.castai_no_match)) }
        items(shown, key = { it.namespace + "/" + it.name }) { rec ->
            RecommendationRow(rec)
            HorizontalDivider()
        }
    }
}

/** What CAST AI changes across the cluster: requests added or saved, over the workloads it compared. */
@Composable
private fun TotalsCard(status: CastAIStatus) {
    Card(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp)) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text(pluralStringResource(R.plurals.castai_workloads, status.recommendations.size, status.recommendations.size), style = MaterialTheme.typography.titleMedium)
            if (status.compared > 0) {
                Row(horizontalArrangement = Arrangement.spacedBy(16.dp)) {
                    DeltaText(stringResource(R.string.castai_cpu_requests), formatMilliCores(status.cpuDeltaMilli, signed = true), status.cpuDeltaMilli)
                    DeltaText(stringResource(R.string.castai_memory_requests), signedBytes(status.memoryDeltaBytes), status.memoryDeltaBytes)
                }
                Text(
                    pluralStringResource(R.plurals.castai_compared, status.compared, status.compared),
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }
}

@Composable
private fun DeltaText(title: String, value: String, delta: Long) {
    val colors = LocalStatusColors.current
    Column {
        Text(title, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        Text(
            value,
            style = MaterialTheme.typography.titleLarge,
            fontFamily = FontFamily.Monospace,
            color = when {
                delta < 0 -> colors.ok
                delta > 0 -> colors.warn
                else -> MaterialTheme.colorScheme.onSurface
            },
        )
    }
}

@Composable
private fun RecommendationRow(rec: CastAIRecommendation) {
    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
        val details = listOfNotNull(rec.kind.takeIf { it.isNotEmpty() }, modeText(rec.applyMode)) + rec.reasonList.map { reasonText(it) }
        ServiceRowHeader(rec.serviceHealth, rec.label, details)
        if (rec.message.isNotEmpty()) {
            Text(rec.message, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(start = 22.dp))
        }
        rec.containers.forEach { ContainerLine(it, rec.containers.size > 1) }
    }
}

/** "cpu 500m → 120m · memory 1Gi → 640Mi", the original first when CAST AI recorded it. */
@Composable
private fun ContainerLine(c: CastAIContainer, named: Boolean) {
    val cpu = change(c.originalCpu, c.cpu)
    val memory = change(c.originalMemory, c.memory)
    val parts = listOfNotNull(
        c.name.takeIf { named },
        cpu?.let { stringResource(R.string.castai_cpu_line, it) },
        memory?.let { stringResource(R.string.castai_memory_line, it) },
    )
    if (parts.isEmpty()) return
    Text(
        parts.joinToString(" · "),
        style = MaterialTheme.typography.bodySmall,
        fontFamily = FontFamily.Monospace,
        modifier = Modifier.padding(start = 22.dp),
    )
}

private fun change(from: String, to: String): String? = when {
    to.isEmpty() -> null
    from.isEmpty() || from == to -> to
    else -> "$from → $to"
}

private fun signedBytes(bytes: Long): String = when {
    bytes < 0 -> "-" + formatBytes(-bytes)
    bytes > 0 -> "+" + formatBytes(bytes)
    else -> formatBytes(0)
}

@Composable
private fun modeText(mode: CastAIMode): String? = when (mode) {
    CastAIMode.IMMEDIATE -> stringResource(R.string.castai_mode_immediate)
    CastAIMode.DEFERRED -> stringResource(R.string.castai_mode_deferred)
    CastAIMode.UNKNOWN -> null
}

@Composable
private fun reasonText(reason: CastAIReason): String = when (reason) {
    CastAIReason.VPA -> stringResource(R.string.castai_reason_vpa)
    CastAIReason.HPA -> stringResource(R.string.castai_reason_hpa)
    CastAIReason.READ_ONLY -> stringResource(R.string.castai_reason_read_only)
}
