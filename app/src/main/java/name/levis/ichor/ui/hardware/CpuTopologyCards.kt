package name.levis.ichor.ui.hardware

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.CpuScalingPolicy
import name.levis.ichor.model.GOVERNOR_POWERSAVE
import name.levis.ichor.model.NumaNode
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.formatBytes

/**
 * The cpufreq policies of a Talos 1.15 node: governor, the CPUs each drives, the frequency
 * window, and the CPUScalingConfig document that set it. [powersaveHint] adds a line when a
 * powersave policy meets a throttled node: the classic cause of a slow node.
 */
@Composable
fun CpuScalingRows(policies: List<CpuScalingPolicy>, powersaveHint: Boolean) {
    if (powersaveHint) {
        Text(
            stringResource(R.string.hardware_powersave_hint),
            color = LocalStatusColors.current.warn,
            style = MaterialTheme.typography.bodySmall,
        )
    }
    policies.forEachIndexed { i, policy ->
        if (i > 0) HorizontalDivider()
        CpuScalingRow(policy)
    }
}

@Composable
private fun CpuScalingRow(p: CpuScalingPolicy) {
    val colors = LocalStatusColors.current
    Column(Modifier.padding(vertical = 4.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                listOf(p.policy, p.cpus).filter { it.isNotBlank() }.joinToString("  ·  "),
                style = MaterialTheme.typography.bodyMedium,
                fontFamily = FontFamily.Monospace,
                modifier = Modifier.weight(1f),
            )
            if (p.coreType.isNotBlank()) StatusPill(p.coreType, colors.muted, Modifier.padding(start = 8.dp))
            if (p.governor.isNotBlank()) {
                val color = if (p.governor == GOVERNOR_POWERSAVE) colors.warn else MaterialTheme.colorScheme.primary
                StatusPill(p.governor, color, Modifier.padding(start = 8.dp))
            }
        }
        val window = if (p.maxKhz > 0) stringResource(R.string.hardware_freq_range, (p.minKhz / 1000).toInt(), (p.maxKhz / 1000).toInt()) else null
        val epp = p.energyPreference.ifBlank { null }?.let { "EPP $it" }
        MutedText(listOfNotNull(window, epp, p.driver.ifBlank { null }).joinToString("  ·  "))
        if (p.configuredBy.isNotBlank()) MutedText(stringResource(R.string.hardware_cpu_scaling_set_by, p.configuredBy))
    }
}

/** One row per NUMA node: its id, CPU ranges and memory. */
@Composable
fun NumaRows(nodes: List<NumaNode>) {
    nodes.forEach { n ->
        Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.fillMaxWidth().padding(vertical = 3.dp)) {
            Column(Modifier.weight(1f)) {
                Text(stringResource(R.string.hardware_numa_node, n.id), style = MaterialTheme.typography.bodyMedium, fontWeight = FontWeight.Bold)
                MutedText(listOf(stringResource(R.string.hardware_cpu_count, n.cpuCount), n.cpus).joinToString("  ·  "))
            }
            if (n.memoryBytes > 0) {
                Text(formatBytes(n.memoryBytes), style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace)
            }
        }
    }
}
