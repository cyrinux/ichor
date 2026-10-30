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
import name.levis.ichor.ui.design.IchorAlertDialog
import name.levis.ichor.ui.design.IchorCard
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import name.levis.ichor.ui.design.IchorTextButton
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
import name.levis.ichor.ui.components.SectionHeader
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.components.expandable

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
 * Per-node clock offset against NTP, like `talosctl time` on every node, under the overview
 * cards' header. Collapsed to its header while every reachable clock is in sync; the nodes
 * that did not answer are left to the summary and Nodes cards.
 */
@Composable
fun TimeDriftCard(state: UiState<ClusterTime>, hostnames: Map<String, String>) {
    val summary = (state as? UiState.Loaded)?.data?.nodes?.let(::driftSummary)
    var expanded by rememberSaveable(summary?.expandedByDefault) { mutableStateOf(summary?.expandedByDefault == true) }
    val level = summary?.level
    val loading = state is UiState.Loading
    val pill: (@Composable () -> Unit)? = if (loading || level != null) {
        {
            if (level == null) CircularProgressIndicator(Modifier.size(16.dp), strokeWidth = 2.dp)
            else StatusPill(stringResource(levelLabel(level)), driftColor(level))
        }
    } else {
        null
    }
    IchorCard(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            // The header toggles the per-node list.
            SectionHeader(
                stringResource(R.string.time_drift_title),
                Modifier.fillMaxWidth().expandable(
                    expanded,
                    enabled = summary != null,
                    actionLabel = stringResource(if (expanded) R.string.time_drift_collapse else R.string.time_drift_expand),
                ) { expanded = !expanded },
                subtitle = summary?.let { summaryText(it) },
                pill = pill,
                chevron = when {
                    summary == null -> null
                    expanded -> Icons.Outlined.ExpandLess
                    else -> Icons.Outlined.ExpandMore
                },
            )
            if (state is UiState.Failed) {
                Text(state.message.asString(), color = LocalStatusColors.current.bad, style = MaterialTheme.typography.bodySmall)
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

/** "7 nodes in sync · max ±9 ms"; null when no clock answered. */
@Composable
private fun summaryText(summary: DriftSummary): String? {
    if (summary.reachable == 0) return null
    val drifting = summary.drifting
    val count = if (drifting == 0) {
        pluralStringResource(R.plurals.time_drift_summary_in_sync, summary.reachable, summary.reachable)
    } else {
        pluralStringResource(R.plurals.time_drift_summary_drifting, drifting, drifting)
    }
    val max = summary.maxOffsetMs?.let { stringResource(R.string.time_drift_max, formatMaxOffset(it)) }
    return listOfNotNull(count, max).joinToString(" · ")
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
        IchorAlertDialog(
            onDismissRequest = { showError = false },
            title = { Text(hostname) },
            text = { Text(t.error.orEmpty(), style = MaterialTheme.typography.bodySmall) },
            confirmButton = { IchorTextButton(onClick = { showError = false }) { Text(stringResource(R.string.common_ok)) } },
        )
    }
}
