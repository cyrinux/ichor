package name.levis.ichor.ui.overview

import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.HealthAndSafety
import androidx.compose.material.icons.outlined.Memory
import androidx.compose.material.icons.outlined.Speed
import androidx.compose.material.icons.outlined.ViewInAr
import androidx.compose.material3.Card
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.res.vectorResource
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.KubeClusterSummary
import name.levis.ichor.model.KubeNodesOverview
import name.levis.ichor.model.KubeTopNodes
import name.levis.ichor.model.clusterLogo
import name.levis.ichor.model.clusterSummary
import name.levis.ichor.ui.components.BundledLogo
import name.levis.ichor.ui.components.StatTile
import name.levis.ichor.ui.components.StatTileRow
import name.levis.ichor.ui.components.UsageBar
import name.levis.ichor.ui.components.rememberKubeWhoAmI
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.formatBytes
import kotlin.math.roundToInt

private const val UNKNOWN = "—"

/**
 * The cluster at a glance, as the Talos home's ClusterSummaryCard: its name, "9 nodes ·
 * Kubernetes v1.31.2" and the overall state, who the API server and the credentials are on one
 * line, then the nodes ready, the cores and the memory, in use when metrics-server answers
 * ([top]). Its footer opens the checkup ([onCheckup]) and the Capacity screen ([onCapacity]).
 * [nodes] is null until the API server answered.
 */
@Composable
internal fun KubeSummaryCard(
    name: String,
    cluster: ContextSummary,
    nodes: KubeNodesOverview?,
    top: KubeTopNodes?,
    onCheckup: () -> Unit,
    onCapacity: () -> Unit,
) {
    val listed = nodes?.takeIf { !it.forbidden && it.nodes.isNotEmpty() }
    val summary = remember(listed, top) { listed?.clusterSummary(top) }
    Card(Modifier.fillMaxWidth()) {
        SummaryIdentityBand(name, leading = { BundledLogo(clusterLogo(cluster), Modifier.padding(end = 8.dp).size(24.dp)) })
        Column(Modifier.padding(start = 16.dp, end = 16.dp, top = 12.dp, bottom = 8.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    headline(summary, nodes?.serverVersion.orEmpty()),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.weight(1f),
                )
                summary?.let { ClusterStatusPill(it.status) }
            }
            IdentityLine(cluster)
            HorizontalDivider(Modifier.padding(vertical = 12.dp))
            StatTileRow(
                Modifier,
                { StatTile(Icons.Outlined.ViewInAr, summary?.let { s -> "${s.ready}/${s.total}" } ?: UNKNOWN, stringResource(R.string.overview_stat_nodes), it, iconTile = true) },
                { CpuStat(summary, it) },
                { MemoryStat(summary, it) },
            )
            Row(Modifier.fillMaxWidth().padding(top = 8.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                SummaryFooterButton(Icons.Outlined.HealthAndSafety, stringResource(R.string.checkup_title), onCheckup)
                SummaryFooterButton(Icons.Outlined.Speed, stringResource(R.string.capacity_title), onCapacity)
            }
        }
    }
}

/** "9 nodes · 1 not ready · Kubernetes v1.31.2"; the version alone while no node is listed. */
@Composable
private fun headline(summary: KubeClusterSummary?, version: String): AnnotatedString {
    val kube = if (version.isNotEmpty()) stringResource(R.string.kube_home_version, version) else stringResource(R.string.common_kind_kubernetes)
    val warn = LocalStatusColors.current.warn
    return buildAnnotatedString {
        if (summary != null) {
            append(pluralStringResource(R.plurals.overview_summary_nodes, summary.total, summary.total))
            if (summary.notReady > 0) {
                append(SUMMARY_SEPARATOR)
                withStyle(SpanStyle(color = warn)) {
                    append("${summary.notReady} ${pluralStringResource(R.plurals.overview_summary_not_ready, summary.notReady)}")
                }
            }
            append(SUMMARY_SEPARATOR)
        }
        append(kube)
    }
}

/** The API server's host and who the credentials are (as the API server takes them, else the kubeconfig's user). */
@Composable
private fun IdentityLine(cluster: ContextSummary) {
    val server = cluster.endpoints.firstOrNull()?.substringAfter("://")?.substringBefore('/').orEmpty()
    val user = rememberKubeWhoAmI(cluster.name)?.user?.takeIf { it.isNotEmpty() } ?: cluster.user
    val line = listOf(server, user).filter { it.isNotEmpty() }.joinToString(SUMMARY_SEPARATOR)
    if (line.isNotEmpty()) {
        Text(
            line,
            style = MaterialTheme.typography.bodySmall,
            fontFamily = FontFamily.Monospace,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
            modifier = Modifier.padding(top = 4.dp),
        )
    }
}

/** Cores; with metrics-server, the busy share of the allocatable ones. */
@Composable
private fun CpuStat(summary: KubeClusterSummary?, modifier: Modifier) {
    val cores = summary?.cpuCount?.takeIf { it > 0 }
    val used = summary?.cpuFraction
    if (used == null) {
        StatTile(Icons.Outlined.Memory, cores?.toString() ?: UNKNOWN, stringResource(R.string.overview_stat_cpu), modifier, iconTile = true)
    } else {
        val shown by animateFloatAsState(used, label = "cpu")
        StatTile(
            Icons.Outlined.Memory,
            stringResource(R.string.overview_stat_percent, (used * 100).roundToInt()),
            cores?.let { pluralStringResource(R.plurals.overview_stat_cpu_of_cores, it, it) } ?: stringResource(R.string.overview_stat_cpu_used),
            modifier,
            iconTile = true,
            meter = { UsageBar(shown, Modifier.padding(top = 6.dp)) },
        )
    }
}

@Composable
private fun MemoryStat(summary: KubeClusterSummary?, modifier: Modifier) {
    val total = summary?.memTotal ?: 0L
    val used = summary?.memFraction
    val shown by animateFloatAsState(used ?: 0f, label = "memory")
    StatTile(
        ImageVector.vectorResource(R.drawable.ic_memory_alt),
        if (total > 0) formatBytes(total) else UNKNOWN,
        used?.let { stringResource(R.string.overview_stat_memory_used, (it * 100).roundToInt()) } ?: stringResource(R.string.overview_stat_memory),
        modifier,
        iconTile = true,
        meter = used?.let { { UsageBar(shown, Modifier.padding(top = 6.dp)) } },
    )
}
