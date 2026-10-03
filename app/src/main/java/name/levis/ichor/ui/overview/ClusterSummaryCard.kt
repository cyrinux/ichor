package name.levis.ichor.ui.overview

import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.KeyboardArrowRight
import androidx.compose.material.icons.outlined.Insights
import androidx.compose.material.icons.outlined.Memory
import androidx.compose.material.icons.outlined.SdCard
import androidx.compose.material.icons.outlined.ViewInAr
import androidx.compose.material3.Card
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.ClusterStatus
import name.levis.ichor.model.ClusterSummary
import name.levis.ichor.model.ClusterUsage
import name.levis.ichor.model.displayVersion
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.components.UsageBar
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.formatBytes
import kotlin.math.roundToInt

/** Unknown capacity (older core, or no node said): a dash rather than a misleading 0. */
private const val UNKNOWN = "—"

/**
 * The cluster at a glance, above the nodes: its name and overall state, how many nodes on
 * which Talos version, and the capacity of the nodes that answered. With [live] usage, CPU
 * and memory follow it; without, they show the overview's snapshot. Tapping it opens the
 * cluster insights, which its footer names.
 */
@Composable
fun ClusterSummaryCard(
    name: String,
    summary: ClusterSummary,
    live: ClusterLiveState?,
    onInsights: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Card(onClick = onInsights, modifier = modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) {
                    Text(name, style = MaterialTheme.typography.titleMedium, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    Text(
                        headline(summary),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                ClusterStatusPill(summary.status)
            }
            Breakdown(summary)
            HorizontalDivider(Modifier.padding(vertical = 12.dp))
            Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                Stat(
                    Icons.Outlined.ViewInAr,
                    "${summary.ready}/${summary.total}",
                    stringResource(R.string.overview_stat_nodes),
                    Modifier.weight(1f),
                )
                CpuStat(summary.cpuCount, live, Modifier.weight(1f))
                // Live memory only once it covers every node the overview reached, never fewer.
                val reached = summary.total - summary.unreachable
                MemoryStat(summary, live?.usage?.takeIf { it.nodes >= reached }, Modifier.weight(1f))
            }
            InsightsFooter()
        }
    }
}

/** The way into the cluster insights, so the card reads as tappable. */
@Composable
private fun InsightsFooter() {
    val color = MaterialTheme.colorScheme.primary
    Row(Modifier.padding(top = 12.dp), verticalAlignment = Alignment.CenterVertically) {
        Icon(Icons.Outlined.Insights, contentDescription = null, tint = color, modifier = Modifier.size(18.dp))
        Text(
            stringResource(R.string.insights_title),
            style = MaterialTheme.typography.labelLarge,
            color = color,
            modifier = Modifier.weight(1f).padding(start = 8.dp),
        )
        Icon(Icons.AutoMirrored.Outlined.KeyboardArrowRight, contentDescription = null, tint = color)
    }
}

/** "3 nodes · v1.11.2", or "v1.10.4 – v1.11.2" while the nodes run different versions. */
@Composable
private fun headline(summary: ClusterSummary): String {
    val versions = summary.versions.map(::displayVersion)
    val version = if (versions.size > 1) "${versions.first()} – ${versions.last()}" else versions.firstOrNull()
    return listOfNotNull(pluralStringResource(R.plurals.overview_summary_nodes, summary.total, summary.total), version)
        .joinToString("  ·  ")
}

@Composable
private fun ClusterStatusPill(status: ClusterStatus) {
    val colors = LocalStatusColors.current
    when (status) {
        ClusterStatus.HEALTHY -> StatusPill(stringResource(R.string.overview_status_healthy), colors.ok)
        ClusterStatus.DEGRADED -> StatusPill(stringResource(R.string.overview_status_degraded), colors.warn)
        ClusterStatus.DOWN -> StatusPill(stringResource(R.string.overview_status_down), colors.bad)
    }
}

/** Which nodes are in trouble, each count in its own status colour; nothing when all are ready. */
@Composable
private fun Breakdown(summary: ClusterSummary) {
    val colors = LocalStatusColors.current
    val parts = listOfNotNull(
        summary.notReady.takeIf { it > 0 }
            ?.let { "$it ${pluralStringResource(R.plurals.overview_summary_not_ready, it)}" to colors.warn },
        summary.unreachable.takeIf { it > 0 }
            ?.let { "$it ${pluralStringResource(R.plurals.overview_summary_unreachable, it)}" to colors.bad },
    )
    if (parts.isEmpty()) return
    Row(Modifier.padding(top = 6.dp)) {
        parts.forEachIndexed { i, (text, color) ->
            if (i > 0) Text("  ·  ", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            Text(text, style = MaterialTheme.typography.bodySmall, color = color)
        }
    }
}

/** Cores; with live usage, the busy share of them and how it moved over the last minutes. */
@Composable
private fun CpuStat(cpuCount: Int, live: ClusterLiveState?, modifier: Modifier) {
    val used = live?.usage?.cpuFraction
    val cores = cpuCount.takeIf { it > 0 }
    Column(modifier) {
        if (used == null) {
            Stat(Icons.Outlined.Memory, cores?.toString() ?: UNKNOWN, stringResource(R.string.overview_stat_cpu))
        } else {
            Stat(
                Icons.Outlined.Memory,
                stringResource(R.string.overview_stat_percent, (used * 100).roundToInt()),
                cores?.let { pluralStringResource(R.plurals.overview_stat_cpu_of_cores, it, it) }
                    ?: stringResource(R.string.overview_stat_cpu_used),
            )
            CpuSparkline(live.cpuHistory, Modifier.padding(top = 4.dp))
        }
    }
}

@Composable
private fun MemoryStat(summary: ClusterSummary, live: ClusterUsage?, modifier: Modifier) {
    val total = live?.memTotal?.takeIf { it > 0 } ?: summary.memTotal
    val used = live?.memUsedFraction ?: summary.memUsedFraction
    // Glide between samples rather than jump.
    val shown by animateFloatAsState(used ?: 0f, label = "memory")
    Column(modifier) {
        Stat(
            Icons.Outlined.SdCard,
            if (total > 0) formatBytes(total) else UNKNOWN,
            used?.let { stringResource(R.string.overview_stat_memory_used, (it * 100).roundToInt()) }
                ?: stringResource(R.string.overview_stat_memory),
        )
        if (used != null) UsageBar(shown, Modifier.padding(top = 6.dp))
    }
}

@Composable
private fun Stat(icon: ImageVector, value: String, label: String, modifier: Modifier = Modifier) {
    Row(modifier, verticalAlignment = Alignment.CenterVertically) {
        Icon(
            icon,
            contentDescription = null,
            tint = MaterialTheme.colorScheme.primary,
            modifier = Modifier.padding(end = 8.dp).size(20.dp),
        )
        // Narrow phones and long translations wrap rather than cut the label short.
        Column {
            Text(value, style = MaterialTheme.typography.titleMedium)
            Text(label, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
}
