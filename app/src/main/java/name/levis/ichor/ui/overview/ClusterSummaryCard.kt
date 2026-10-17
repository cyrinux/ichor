package name.levis.ichor.ui.overview

import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Insights
import androidx.compose.material.icons.outlined.Memory
import androidx.compose.material.icons.outlined.Speed
import androidx.compose.material.icons.outlined.ViewInAr
import androidx.compose.material3.Card
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.res.vectorResource
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.ClusterStatus
import name.levis.ichor.model.ClusterSummary
import name.levis.ichor.model.ClusterUsage
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.NodesAnswered
import name.levis.ichor.model.clusterLogo
import name.levis.ichor.model.displayVersion
import name.levis.ichor.ui.components.BundledLogo
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.StatTile
import name.levis.ichor.ui.components.StatTileRow
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.components.UsageBar
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.formatBytes
import kotlin.math.roundToInt

/** Unknown capacity (older core, or no node said): a dash rather than a misleading 0. */
private const val UNKNOWN = "—"

private const val SEPARATOR = "  ·  "

/**
 * The cluster at a glance, above the nodes: its name and overall state, how many nodes on
 * which Talos version, and the capacity of the nodes that answered. With [live] usage, CPU
 * and memory follow it; without, they show the overview's snapshot. The card itself does not
 * open anything: its footer's two labelled buttons open the cluster insights ([onInsights])
 * and the Capacity screen ([onCapacity]).
 */
@Composable
fun ClusterSummaryCard(
    name: String,
    summary: ClusterSummary,
    live: ClusterLiveState?,
    /** While the overview streams in: how many nodes answered, shown instead of the health badge. */
    answered: NodesAnswered?,
    onInsights: () -> Unit,
    onCapacity: () -> Unit,
    /** The active context, for the provider logo before the name; none: no logo. */
    context: ContextSummary? = null,
    modifier: Modifier = Modifier,
) {
    val troubles = troubles(summary)
    Card(modifier = modifier.fillMaxWidth()) {
        // A fixed box, so the name does not shift once the logo has loaded.
        SummaryIdentityBand(name, leading = context?.let { { BundledLogo(clusterLogo(it), Modifier.padding(end = 8.dp).size(24.dp)) } })
        Column(Modifier.padding(start = 16.dp, end = 16.dp, top = 12.dp, bottom = 8.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                // One trouble reads in the headline; two get their own line below it.
                Text(
                    headline(summary, troubles.singleOrNull()),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.weight(1f),
                )
                if (answered == null) ClusterStatusPill(summary.status)
            }
            if (answered != null) AnsweredProgress(answered) else if (troubles.size > 1) Breakdown(troubles)
            HorizontalDivider(Modifier.padding(vertical = 12.dp))
            // Live memory only once it covers every node the overview reached, never fewer.
            val reached = summary.total - summary.unreachable
            StatTileRow(
                Modifier,
                { StatTile(Icons.Outlined.ViewInAr, "${summary.ready}/${summary.total}", stringResource(R.string.overview_stat_nodes), it, iconTile = true) },
                { CpuStat(summary.cpuCount, live, it) },
                { MemoryStat(summary, live?.usage?.takeIf { usage -> usage.nodes >= reached }, it) },
            )
            Footer(onInsights, onCapacity)
        }
    }
}

/** The card's two actions, each a labelled button so TalkBack reads them by name. */
@Composable
private fun Footer(onInsights: () -> Unit, onCapacity: () -> Unit) {
    Row(Modifier.fillMaxWidth().padding(top = 8.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        FooterButton(Icons.Outlined.Insights, stringResource(R.string.overview_action_insights), onInsights)
        FooterButton(Icons.Outlined.Speed, stringResource(R.string.capacity_title), onCapacity)
    }
}

@Composable
private fun FooterButton(icon: ImageVector, label: String, onClick: () -> Unit) {
    TextButton(onClick = onClick, modifier = Modifier.heightIn(min = 48.dp)) {
        Icon(icon, contentDescription = null, modifier = Modifier.size(18.dp))
        Text(label, modifier = Modifier.padding(start = 8.dp), maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}

/**
 * "7 of 8 nodes answered" over a thin bar (as the paged lists show their pages), or the count
 * alone over a running bar when the context does not say how many nodes it has.
 */
@Composable
private fun AnsweredProgress(answered: NodesAnswered) {
    val total = answered.total
    Column(Modifier.fillMaxWidth().padding(top = 8.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        MutedText(
            if (total != null) stringResource(R.string.overview_nodes_answered, answered.answered, total)
            else stringResource(R.string.overview_nodes_answered_unknown, answered.answered),
        )
        if (total != null) {
            LinearProgressIndicator(progress = { answered.answered.toFloat() / total }, modifier = Modifier.fillMaxWidth())
        } else {
            LinearProgressIndicator(Modifier.fillMaxWidth())
        }
    }
}

/** A count of nodes in trouble ("1 unreachable") and its status colour. */
private data class Trouble(val text: String, val color: Color)

/** The nodes not ready, then those unreachable, each when there are any. */
@Composable
private fun troubles(summary: ClusterSummary): List<Trouble> {
    val colors = LocalStatusColors.current
    return listOfNotNull(
        summary.notReady.takeIf { it > 0 }
            ?.let { Trouble("$it ${pluralStringResource(R.plurals.overview_summary_not_ready, it)}", colors.warn) },
        summary.unreachable.takeIf { it > 0 }
            ?.let { Trouble("$it ${pluralStringResource(R.plurals.overview_summary_unreachable, it)}", colors.bad) },
    )
}

/**
 * "8 nodes · 1 unreachable · v1.14.1 – v1.14.2": the node count, [trouble] in its colour when
 * it is the only one, and the version, or the span while the nodes run different versions.
 */
@Composable
private fun headline(summary: ClusterSummary, trouble: Trouble?): AnnotatedString {
    val versions = summary.versions.map(::displayVersion)
    val version = if (versions.size > 1) "${versions.first()} – ${versions.last()}" else versions.firstOrNull()
    return buildAnnotatedString {
        append(pluralStringResource(R.plurals.overview_summary_nodes, summary.total, summary.total))
        if (trouble != null) {
            append(SEPARATOR)
            withStyle(SpanStyle(color = trouble.color)) { append(trouble.text) }
        }
        if (version != null) append(SEPARATOR + version)
    }
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

/** Nodes both not ready and unreachable: each count in its own status colour, on its own line. */
@Composable
private fun Breakdown(troubles: List<Trouble>) {
    Row(Modifier.padding(top = 6.dp)) {
        troubles.forEachIndexed { i, (text, color) ->
            if (i > 0) MutedText(SEPARATOR)
            Text(text, style = MaterialTheme.typography.bodySmall, color = color)
        }
    }
}

/** Cores; with live usage, the busy share of them and how it moved over the last minutes. */
@Composable
private fun CpuStat(cpuCount: Int, live: ClusterLiveState?, modifier: Modifier) {
    val used = live?.usage?.cpuFraction
    val cores = cpuCount.takeIf { it > 0 }
    if (used == null) {
        StatTile(Icons.Outlined.Memory, cores?.toString() ?: UNKNOWN, stringResource(R.string.overview_stat_cpu), modifier, iconTile = true)
    } else {
        StatTile(
            Icons.Outlined.Memory,
            stringResource(R.string.overview_stat_percent, (used * 100).roundToInt()),
            cores?.let { pluralStringResource(R.plurals.overview_stat_cpu_of_cores, it, it) }
                ?: stringResource(R.string.overview_stat_cpu_used),
            modifier,
            iconTile = true,
        ) { CpuSparkline(live.cpuHistory, Modifier.padding(top = 4.dp)) }
    }
}

@Composable
private fun MemoryStat(summary: ClusterSummary, live: ClusterUsage?, modifier: Modifier) {
    val total = live?.memTotal?.takeIf { it > 0 } ?: summary.memTotal
    val used = live?.memUsedFraction ?: summary.memUsedFraction
    // Glide between samples rather than jump.
    val shown by animateFloatAsState(used ?: 0f, label = "memory")
    StatTile(
        ImageVector.vectorResource(R.drawable.ic_memory_alt),
        if (total > 0) formatBytes(total) else UNKNOWN,
        used?.let { stringResource(R.string.overview_stat_memory_used, (it * 100).roundToInt()) }
            ?: stringResource(R.string.overview_stat_memory),
        modifier,
        iconTile = true,
        meter = used?.let { { UsageBar(shown, Modifier.padding(top = 6.dp)) } },
    )
}
