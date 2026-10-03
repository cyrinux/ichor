package name.levis.ichor.ui.overview

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.ExpandLess
import androidx.compose.material.icons.outlined.ExpandMore
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Card
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
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
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.data.CLUSTER_TIME
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.model.ClusterTime
import name.levis.ichor.model.DriftLevel
import name.levis.ichor.model.NodeTime
import name.levis.ichor.model.drift
import name.levis.ichor.model.formatOffset
import name.levis.ichor.model.DriftSummary
import name.levis.ichor.model.driftSummary
import name.levis.ichor.model.formatMaxOffset
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.theme.LocalStatusColors

class ClusterTimeViewModel(private val talos: TalosRepository) : LoadingViewModel<ClusterTime>() {
    override fun cached(): TalosRepository.Timed<ClusterTime>? = talos.cached(CLUSTER_TIME)
    override val restores get() = talos.restores
    override suspend fun fetch() = talos.clusterTime()
}

@Composable
fun driftColor(level: DriftLevel): Color {
    val colors = LocalStatusColors.current
    return when (level) {
        DriftLevel.OK -> colors.ok
        DriftLevel.WARN -> colors.warn
        DriftLevel.BAD -> colors.bad
    }
}

/**
 * Per-node clock offset against NTP, like `talosctl time` on every node. Collapsed to one
 * line while every reachable clock is in sync; unreachable nodes are counted apart.
 */
@Composable
fun TimeDriftCard(state: UiState<ClusterTime>, hostnames: Map<String, String>) {
    val summary = (state as? UiState.Loaded)?.data?.nodes?.let(::driftSummary)
    var expanded by rememberSaveable(summary?.expandedByDefault) { mutableStateOf(summary?.expandedByDefault == true) }
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            // Title and summary toggle the per-node list.
            Column(
                Modifier.fillMaxWidth().clickable(enabled = summary != null) { expanded = !expanded },
                verticalArrangement = Arrangement.spacedBy(4.dp),
            ) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(stringResource(R.string.time_drift_title), style = MaterialTheme.typography.titleSmall, modifier = Modifier.weight(1f))
                    when {
                        state is UiState.Loading -> CircularProgressIndicator(Modifier.size(16.dp), strokeWidth = 2.dp)
                        summary?.level != null -> StatusPill(stringResource(levelLabel(summary.level)), driftColor(summary.level))
                    }
                    if (summary != null) {
                        Icon(
                            if (expanded) Icons.Outlined.ExpandLess else Icons.Outlined.ExpandMore,
                            stringResource(if (expanded) R.string.time_drift_collapse else R.string.time_drift_expand),
                            tint = MaterialTheme.colorScheme.onSurfaceVariant,
                            modifier = Modifier.padding(start = 8.dp),
                        )
                    }
                }
                if (state is UiState.Failed) {
                    Text(state.message.asString(), color = LocalStatusColors.current.bad, style = MaterialTheme.typography.bodySmall)
                }
                summary?.let { SummaryLine(it) }
            }
            if (expanded && state is UiState.Loaded) {
                state.data.nodes
                    .sortedBy { hostnames[it.node] ?: it.node }
                    .forEach { NodeTimeRow(it, hostnames[it.node] ?: it.node) }
            }
        }
    }
}

private fun levelLabel(level: DriftLevel) = when (level) {
    DriftLevel.OK -> R.string.time_drift_in_sync
    DriftLevel.WARN -> R.string.time_drift_drifting
    DriftLevel.BAD -> R.string.time_drift_bad
}

/** "7 nodes in sync · max ±9 ms", then a neutral "1 unreachable" when some did not answer. */
@Composable
private fun SummaryLine(summary: DriftSummary) {
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    if (summary.reachable > 0) {
        val drifting = summary.drifting
        val count = if (drifting == 0) {
            pluralStringResource(R.plurals.time_drift_summary_in_sync, summary.reachable, summary.reachable)
        } else {
            pluralStringResource(R.plurals.time_drift_summary_drifting, drifting, drifting)
        }
        val max = summary.maxOffsetMs?.let { stringResource(R.string.time_drift_max, formatMaxOffset(it)) }
        Text(listOfNotNull(count, max).joinToString(" · "), style = MaterialTheme.typography.bodySmall, color = muted)
    }
    if (summary.unreachable > 0) {
        Text(
            pluralStringResource(R.plurals.time_drift_unreachable_count, summary.unreachable, summary.unreachable),
            style = MaterialTheme.typography.bodySmall,
            color = muted,
        )
    }
}

@Composable
private fun NodeTimeRow(t: NodeTime, hostname: String) {
    var showError by remember { mutableStateOf(false) }
    val level = t.drift
    Row(
        Modifier.fillMaxWidth().clickable(enabled = t.error != null) { showError = true }.padding(vertical = 2.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(hostname, style = MaterialTheme.typography.bodyMedium, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
        Text(
            if (level == null) stringResource(R.string.time_drift_unreachable) else formatOffset(t.offsetMs),
            style = MaterialTheme.typography.bodyMedium,
            fontFamily = if (level == null) FontFamily.Default else FontFamily.Monospace,
            color = level?.let { driftColor(it) } ?: MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
    if (showError) {
        AlertDialog(
            onDismissRequest = { showError = false },
            title = { Text(hostname) },
            text = { Text(t.error.orEmpty(), style = MaterialTheme.typography.bodySmall) },
            confirmButton = { TextButton(onClick = { showError = false }) { Text(stringResource(R.string.common_ok)) } },
        )
    }
}
