package name.levis.talosmobile.ui.overview

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material3.Card
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.talosmobile.R
import name.levis.talosmobile.data.CLUSTER_TIME
import name.levis.talosmobile.data.TalosRepository
import name.levis.talosmobile.model.ClusterTime
import name.levis.talosmobile.model.DriftLevel
import name.levis.talosmobile.model.NodeTime
import name.levis.talosmobile.model.drift
import name.levis.talosmobile.model.formatOffset
import name.levis.talosmobile.model.worstDrift
import name.levis.talosmobile.ui.LoadingViewModel
import name.levis.talosmobile.ui.UiState
import name.levis.talosmobile.ui.asString
import name.levis.talosmobile.ui.components.StatusPill
import name.levis.talosmobile.ui.theme.LocalStatusColors

class ClusterTimeViewModel(private val talos: TalosRepository) : LoadingViewModel<ClusterTime>() {
    override fun cached(): TalosRepository.Timed<ClusterTime>? = talos.cached(CLUSTER_TIME)
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

/** Per-node clock offset against NTP, like `talosctl time` on every node. */
@Composable
fun TimeDriftCard(state: UiState<ClusterTime>, hostnames: Map<String, String>) {
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(stringResource(R.string.time_drift_title), style = MaterialTheme.typography.titleSmall, modifier = Modifier.weight(1f))
                when (state) {
                    UiState.Loading -> CircularProgressIndicator(Modifier.size(16.dp), strokeWidth = 2.dp)
                    is UiState.Loaded -> {
                        val worst = worstDrift(state.data.nodes)
                        val label = when (worst) {
                            DriftLevel.OK -> R.string.time_drift_in_sync
                            DriftLevel.WARN -> R.string.time_drift_drifting
                            DriftLevel.BAD -> R.string.time_drift_bad
                        }
                        StatusPill(stringResource(label), driftColor(worst))
                    }
                    is UiState.Failed -> Unit
                }
            }
            when (state) {
                UiState.Loading -> Unit
                is UiState.Failed -> Text(state.message.asString(), color = LocalStatusColors.current.bad, style = MaterialTheme.typography.bodySmall)
                is UiState.Loaded -> state.data.nodes
                    .sortedBy { hostnames[it.node] ?: it.node }
                    .forEach { NodeTimeRow(it, hostnames[it.node] ?: it.node) }
            }
        }
    }
}

@Composable
private fun NodeTimeRow(t: NodeTime, hostname: String) {
    Column {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(hostname, style = MaterialTheme.typography.bodyMedium, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
            Text(
                if (t.error == null) formatOffset(t.offsetMs) else stringResource(R.string.time_drift_error),
                style = MaterialTheme.typography.bodyMedium,
                fontFamily = FontFamily.Monospace,
                color = driftColor(t.drift),
            )
        }
        t.error?.let { Text(it, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
    }
}
