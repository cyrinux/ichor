package name.levis.ichor.ui.dataservices

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Card
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.CastAIChange
import name.levis.ichor.model.CastAIContainer
import name.levis.ichor.model.CastAIRecommendation
import name.levis.ichor.model.ServiceHealth
import name.levis.ichor.model.formatMilliCores
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.components.upwardScrollStaysInSheet
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.formatBytes

/**
 * One workload's recommendation: how a pod's requests change, each container's requests against
 * the limits CAST AI keeps, and how and whether it is applied.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun CastAIWorkloadSheet(rec: CastAIRecommendation, onDismiss: () -> Unit) {
    ModalBottomSheet(onDismissRequest = onDismiss, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)) {
        LazyColumn(
            Modifier.upwardScrollStaysInSheet(),
            contentPadding = PaddingValues(start = 16.dp, end = 16.dp, bottom = 24.dp),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            item { Header(rec) }
            item { Pills(rec) }
            if (rec.message.isNotEmpty()) item {
                if (rec.serviceHealth == ServiceHealth.CRITICAL) CauseBanner(rec.message) else WarnBanner(rec.message)
            }
            if (rec.nearMemoryLimit) item { WarnBanner(stringResource(R.string.castai_near_limit_detail, rec.memoryLimitPercent.toString())) }
            item { SectionTitle(stringResource(R.string.castai_per_pod)) }
            item { PerPod(rec) }
            item { SectionTitle(stringResource(R.string.castai_containers)) }
            items(rec.containers, key = { it.name }) { ContainerCard(it) }
        }
    }
}

@Composable
private fun Header(rec: CastAIRecommendation) {
    Column {
        Text(rec.workload.ifEmpty { rec.name }, style = MaterialTheme.typography.titleMedium, fontFamily = FontFamily.Monospace)
        Text(listOf(rec.namespace, rec.kind).filter { it.isNotEmpty() }.joinToString(" · "), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun Pills(rec: CastAIRecommendation) {
    val colors = LocalStatusColors.current
    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
        if (rec.reasonList.isEmpty()) StatusPill(stringResource(R.string.castai_healthy), colors.ok)
        rec.reasonList.forEach { StatusPill(castAIReasonText(it), if (rec.serviceHealth == ServiceHealth.CRITICAL) colors.bad else colors.warn) }
        castAIModeText(rec.applyMode)?.let { StatusPill(it, colors.muted) }
    }
}

/** A pod's CPU and memory requests, before and after, with the change. */
@Composable
private fun PerPod(rec: CastAIRecommendation) {
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(16.dp)) {
            val known = rec.change != CastAIChange.UNKNOWN
            BigChange(
                stringResource(R.string.castai_cpu_requests), formatMilliCores(rec.originalCpuMilli), formatMilliCores(rec.cpuMilli),
                formatMilliCores(rec.cpuDeltaMilli, signed = true).takeIf { known }, deltaColor(rec.cpuDeltaMilli),
                rec.originalCpuMilli.toDouble(), rec.cpuMilli.toDouble(),
            )
            BigChange(
                stringResource(R.string.castai_memory_requests), formatBytes(rec.originalMemoryBytes), formatBytes(rec.memoryBytes),
                signedBytes(rec.memoryDeltaBytes).takeIf { known }, deltaColor(rec.memoryDeltaBytes),
                rec.originalMemoryBytes.toDouble(), rec.memoryBytes.toDouble(),
            )
        }
    }
}

@Composable
private fun BigChange(title: String, before: String, after: String, delta: String?, deltaColor: Color, beforeValue: Double, afterValue: Double) {
    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
        Row(verticalAlignment = Alignment.Bottom) {
            Text(title, style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.weight(1f))
            delta?.let { Text(it, style = MaterialTheme.typography.labelLarge, fontFamily = FontFamily.Monospace, color = deltaColor) }
        }
        Row(verticalAlignment = Alignment.Bottom, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            if (before != after) {
                Text(before, style = MaterialTheme.typography.titleMedium, fontFamily = FontFamily.Monospace, color = MaterialTheme.colorScheme.onSurfaceVariant)
                Text("→", color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            Text(after, style = MaterialTheme.typography.headlineSmall, fontFamily = FontFamily.Monospace)
        }
        BeforeAfterBar(beforeValue, afterValue, height = 10.dp)
    }
}

/** One container: its requests before and after, and how much of each limit the new request takes. */
@Composable
private fun ContainerCard(c: CastAIContainer) {
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(horizontal = 16.dp, vertical = 12.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text(c.name, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace)
            ContainerLine(stringResource(R.string.castai_cpu), castAIChangeText(c.originalCpu.ifEmpty { c.cpu }, c.cpu), c.cpuLimit, c.cpuLimitPercent, warn = false)
            ContainerLine(stringResource(R.string.castai_memory), castAIChangeText(c.originalMemory.ifEmpty { c.memory }, c.memory), c.memoryLimit, c.memoryLimitPercent, warn = c.nearMemoryLimit)
        }
    }
}

@Composable
private fun ContainerLine(label: String, change: String, limit: String, percent: Int, warn: Boolean) {
    Row(verticalAlignment = Alignment.CenterVertically) {
        Text(label, style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.weight(0.25f))
        Text(change.ifEmpty { "—" }, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace, modifier = Modifier.weight(0.4f))
        Text(
            if (limit.isEmpty()) stringResource(R.string.castai_no_limit) else stringResource(R.string.castai_limit_share, limit, percent.toString()),
            style = MaterialTheme.typography.labelSmall,
            color = if (warn) LocalStatusColors.current.warn else MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.weight(0.35f),
        )
    }
}
