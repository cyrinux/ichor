package name.levis.ichor.ui.overview

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.DeveloperBoard
import androidx.compose.material.icons.outlined.Memory
import androidx.compose.material.icons.outlined.ViewInAr
import androidx.compose.material3.Card
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
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
import name.levis.ichor.model.displayVersion
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.components.UsageBar
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.formatBytes

/** Unknown capacity (older core, or no node said): a dash rather than a misleading 0. */
private const val UNKNOWN = "—"

/**
 * The cluster at a glance, above the nodes: its name and overall state, how many nodes on
 * which Talos version, and the capacity of the nodes that answered.
 */
@Composable
fun ClusterSummaryCard(name: String, summary: ClusterSummary, modifier: Modifier = Modifier) {
    Card(modifier.fillMaxWidth()) {
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
                Stat(
                    Icons.Outlined.DeveloperBoard,
                    summary.cpuCount.takeIf { it > 0 }?.toString() ?: UNKNOWN,
                    stringResource(R.string.overview_stat_cpu),
                    Modifier.weight(1f),
                )
                MemoryStat(summary, Modifier.weight(1f))
            }
        }
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

/** Which nodes are in trouble; nothing when all are ready. */
@Composable
private fun Breakdown(summary: ClusterSummary) {
    val colors = LocalStatusColors.current
    val parts = listOfNotNull(
        summary.notReady.takeIf { it > 0 }?.let { "$it ${pluralStringResource(R.plurals.overview_summary_not_ready, it)}" },
        summary.unreachable.takeIf { it > 0 }?.let { "$it ${pluralStringResource(R.plurals.overview_summary_unreachable, it)}" },
    )
    if (parts.isEmpty()) return
    Text(
        parts.joinToString("  ·  "),
        style = MaterialTheme.typography.bodySmall,
        color = if (summary.unreachable > 0) colors.bad else colors.warn,
        modifier = Modifier.padding(top = 6.dp),
    )
}

@Composable
private fun MemoryStat(summary: ClusterSummary, modifier: Modifier) {
    val used = summary.memUsedFraction
    Column(modifier) {
        Stat(
            Icons.Outlined.Memory,
            if (summary.memTotal > 0) formatBytes(summary.memTotal) else UNKNOWN,
            used?.let { stringResource(R.string.overview_stat_memory_used, (it * 100).toInt()) }
                ?: stringResource(R.string.overview_stat_memory),
        )
        used?.let { UsageBar(it, Modifier.padding(top = 6.dp)) }
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
        Column {
            Text(value, style = MaterialTheme.typography.titleMedium, maxLines = 1)
            Text(
                label,
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }
    }
}
