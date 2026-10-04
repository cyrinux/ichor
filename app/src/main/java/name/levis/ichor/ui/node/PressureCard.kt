package name.levis.ichor.ui.node

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Card
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.model.CgroupAlert
import name.levis.ichor.model.CgroupPsi
import name.levis.ichor.model.CgroupReport
import name.levis.ichor.model.PressureLevel
import name.levis.ichor.model.pressureLevel
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.formatPercent

/** One read of the node's cgroups for the pressure card: loaded with the Resources tab. */
class PressureViewModel(private val talos: TalosRepository, private val node: String) : LoadingViewModel<CgroupReport>() {
    override suspend fun fetch() = talos.cgroups(node)
}

/**
 * The node's pressure (PSI) in node status: how much of the last 10 s tasks waited for CPU,
 * memory and disk, who waited the most, and the workloads OOM-killed or near their memory
 * limit. The per-cgroup detail lives in the Cgroups tab ([onDetails]).
 */
@Composable
fun PressureCard(state: UiState<CgroupReport>, onDetails: () -> Unit) {
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) { SectionTitle(stringResource(R.string.node_section_pressure)) }
                TextButton(onClick = onDetails) { Text(stringResource(R.string.node_pressure_details)) }
            }
            MutedText(stringResource(R.string.node_pressure_caption))
            when (state) {
                UiState.Loading -> Text("…", style = MaterialTheme.typography.bodyMedium)
                is UiState.Failed -> Text(
                    stringResource(R.string.node_pressure_unavailable, state.message.asString()),
                    style = MaterialTheme.typography.bodySmall,
                    color = LocalStatusColors.current.bad,
                )
                is UiState.Loaded -> PressureContent(state.data)
            }
        }
    }
}

@Composable
private fun PressureContent(report: CgroupReport) {
    val most = report.hotspots.associateBy { it.resource }
    PressureRow(stringResource(R.string.node_pressure_cpu), report.pressure.cpu, most["cpu"]?.who)
    PressureRow(stringResource(R.string.node_pressure_memory), report.pressure.memory, most["memory"]?.who)
    PressureRow(stringResource(R.string.node_pressure_io), report.pressure.io, most["io"]?.who)
    report.alerts.forEach { AlertRow(it) }
}

@Composable
private fun PressureRow(label: String, psi: CgroupPsi, mostAffected: String?) {
    val level = pressureLevel(psi.some10)
    Column {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(label, style = MaterialTheme.typography.bodyMedium, modifier = Modifier.weight(1f))
            Text(
                formatPercent(psi.some10),
                style = MaterialTheme.typography.bodyMedium,
                fontFamily = FontFamily.Monospace,
                color = levelColor(level),
            )
        }
        // Who waits the most is only worth naming once the node itself waits.
        if (mostAffected != null && level != PressureLevel.OK) {
            MutedText(stringResource(R.string.node_pressure_most, mostAffected))
        }
    }
}

@Composable
private fun AlertRow(alert: CgroupAlert) {
    val text = when (alert.kind) {
        "oomKill" -> pluralStringResource(R.plurals.node_cgroups_oom, alert.count.toInt(), alert.who, alert.count.toInt())
        else -> stringResource(R.string.node_cgroups_near_limit, alert.who, alert.percent.toInt())
    }
    Text(
        "⚠ $text",
        style = MaterialTheme.typography.bodySmall,
        color = if (alert.kind == "oomKill") LocalStatusColors.current.bad else LocalStatusColors.current.warn,
    )
}

@Composable
fun levelColor(level: PressureLevel): Color = when (level) {
    PressureLevel.OK -> MaterialTheme.colorScheme.onSurface
    PressureLevel.WARN -> LocalStatusColors.current.warn
    PressureLevel.BAD -> LocalStatusColors.current.bad
}
