package name.levis.talosmobile.ui.node

import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import name.levis.talosmobile.R
import name.levis.talosmobile.data.TalosRepository
import name.levis.talosmobile.model.NodeTime
import name.levis.talosmobile.model.drift
import name.levis.talosmobile.model.formatOffset
import name.levis.talosmobile.ui.LoadingViewModel
import name.levis.talosmobile.ui.UiState
import name.levis.talosmobile.ui.overview.driftColor
import name.levis.talosmobile.ui.theme.LocalStatusColors

/** The node's clock offset against its NTP server (not cached: it goes stale fast). */
class NodeTimeViewModel(private val talos: TalosRepository, private val node: String) : LoadingViewModel<NodeTime>() {
    override suspend fun fetch() = talos.nodeTime(node)
}

/** "Clock offset  +12 ms (pool.ntp.org)", coloured by drift; "—" while loading or on failure. */
@Composable
fun ClockOffsetRow(state: UiState<NodeTime>) {
    val time = (state as? UiState.Loaded)?.data
    val value = when {
        time == null -> "—"
        time.error != null -> stringResource(R.string.time_drift_error)
        time.server.isNotEmpty() -> "${formatOffset(time.offsetMs)} (${time.server})"
        else -> formatOffset(time.offsetMs)
    }
    Row(Modifier.fillMaxWidth().padding(vertical = 3.dp)) {
        Text(
            stringResource(R.string.node_clock_offset),
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.weight(0.4f),
        )
        Text(
            value,
            style = MaterialTheme.typography.bodyMedium,
            fontFamily = FontFamily.Monospace,
            color = when {
                time == null -> MaterialTheme.colorScheme.onSurface
                else -> time.drift?.let { driftColor(it) } ?: LocalStatusColors.current.bad
            },
            modifier = Modifier.weight(0.6f),
        )
    }
}
