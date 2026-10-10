package name.levis.ichor.ui.hardware

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Card
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.sensorsKey
import name.levis.ichor.model.CpuFrequency
import name.levis.ichor.model.NodeSensors
import name.levis.ichor.model.PciDevice
import name.levis.ichor.model.SensorSection
import name.levis.ichor.model.TemperatureSensor
import name.levis.ichor.model.hot
import name.levis.ichor.model.limitFraction
import name.levis.ichor.model.sensorsEmpty
import name.levis.ichor.model.sensorsError
import name.levis.ichor.ui.components.InfoRow
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * Whether node was throttled when its Hardware screen last read its sensors (in memory only:
 * the overview never reads sensors itself, so the row says nothing until the screen was opened).
 */
@Composable
fun cachedThrottled(node: String): Boolean {
    val app = LocalContext.current.applicationContext as? TalosApp ?: return false
    return app.talosRepository.cached<NodeSensors>(sensorsKey(node))?.value?.throttled == true
}

/** One decimal, the precision sysfs gives. */
private fun Double.oneDecimal() = "%.1f".format(this)

/** Whether the Sensors card has anything to show: a VM without sensors and errors has none. */
fun NodeSensors.showsSensorsCard() = !sensorsEmpty || sensorsError != null

/** Whether the PCI card has anything to show. */
fun NodeSensors.showsPciCard() = pci.isNotEmpty() || errors[SensorSection.PCI] != null

/**
 * Temperatures with a bar to their limit, fans, per-core CPU frequency with the governor and
 * the throttle counters; voltages and thermal zones behind "more". A "throttled" pill in the
 * title when the node is held back.
 */
@Composable
fun SensorsCard(s: NodeSensors) {
    val colors = LocalStatusColors.current
    var more by rememberSaveable { mutableStateOf(false) }
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) { SectionTitle(stringResource(R.string.hardware_sensors)) }
                if (s.throttled) StatusPill(stringResource(R.string.hardware_throttled), colors.bad)
            }
            if (s.throttled) MutedText(stringResource(R.string.hardware_throttled_hint))
            s.sensorsError?.let { Text(it, color = colors.bad, style = MaterialTheme.typography.bodySmall) }
            s.temperatures.forEach { TemperatureRow(it) }
            if (s.fans.isNotEmpty()) {
                HorizontalDivider(Modifier.padding(vertical = 4.dp))
                s.fans.forEach { InfoRow(sensorName(it.chip, it.label), stringResource(R.string.hardware_rpm, it.rpm.toInt())) }
            }
            if (s.cpuFreq.isNotEmpty()) {
                HorizontalDivider(Modifier.padding(vertical = 4.dp))
                CpuFrequencies(s.cpuFreq)
            }
            s.throttle?.let {
                InfoRow(stringResource(R.string.hardware_throttle_events), stringResource(R.string.hardware_throttle_counts, it.coreEvents.toInt(), it.packageEvents.toInt()))
            }
            if (s.voltages.isNotEmpty() || s.thermalZones.isNotEmpty()) {
                TextButton(onClick = { more = !more }) {
                    Text(stringResource(if (more) R.string.hardware_sensors_less else R.string.hardware_sensors_more))
                }
                if (more) {
                    s.voltages.forEach { InfoRow(sensorName(it.chip, it.label), stringResource(R.string.hardware_volts, "%.3f".format(it.volts)), mono = true) }
                    s.thermalZones.forEach { InfoRow(it.type, stringResource(R.string.hardware_celsius, it.celsius.oneDecimal()), mono = true) }
                }
            }
        }
    }
}

private fun sensorName(chip: String, label: String) = if (chip.isBlank()) label else "$label ($chip)"

@Composable
private fun TemperatureRow(t: TemperatureSensor) {
    val colors = LocalStatusColors.current
    val fraction = t.limitFraction
    val color = when {
        t.hot -> colors.bad
        fraction != null && fraction >= WARM_FRACTION -> colors.warn
        else -> colors.ok
    }
    Column(Modifier.padding(vertical = 3.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(sensorName(t.chip, t.label), style = MaterialTheme.typography.bodyMedium, modifier = Modifier.weight(1f))
            Text(
                stringResource(R.string.hardware_celsius, t.celsius.oneDecimal()),
                style = MaterialTheme.typography.bodyMedium,
                fontFamily = FontFamily.Monospace,
                fontWeight = FontWeight.Bold,
                color = if (t.hot) colors.bad else MaterialTheme.colorScheme.onSurface,
            )
        }
        if (fraction != null) {
            LinearProgressIndicator(
                progress = { fraction },
                color = color,
                strokeCap = StrokeCap.Round,
                modifier = Modifier.fillMaxWidth().padding(vertical = 2.dp),
            )
        }
        val limits = listOfNotNull(
            t.maxCelsius.takeIf { it > 0 }?.let { stringResource(R.string.hardware_temp_max, it.oneDecimal()) },
            t.critCelsius.takeIf { it > 0 }?.let { stringResource(R.string.hardware_temp_crit, it.oneDecimal()) },
        )
        if (limits.isNotEmpty()) MutedText(limits.joinToString("  ·  "))
    }
}

/** A share of the limit from which a temperature reads as warm. */
private const val WARM_FRACTION = 0.85f

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun CpuFrequencies(freqs: List<CpuFrequency>) {
    Text(stringResource(R.string.hardware_cpu_frequency), style = MaterialTheme.typography.bodyMedium, fontWeight = FontWeight.Bold)
    val governors = freqs.map { it.governor }.filter { it.isNotBlank() }.distinct()
    val range = freqs.first().let { stringResource(R.string.hardware_freq_range, it.minMhz.toInt(), it.maxMhz.toInt()) }
    MutedText((governors + range).joinToString("  ·  "))
    FlowRow(horizontalArrangement = Arrangement.spacedBy(12.dp), modifier = Modifier.fillMaxWidth().padding(top = 2.dp)) {
        freqs.forEach { f ->
            Text(
                stringResource(R.string.hardware_cpu_core_mhz, f.cpu, f.currentMhz.toInt()),
                style = MaterialTheme.typography.bodySmall,
                fontFamily = FontFamily.Monospace,
            )
        }
    }
}

/** The node's PCI devices: product, vendor, class and the driver bound to it. */
@Composable
fun PciCard(s: NodeSensors) {
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            SectionTitle(stringResource(R.string.hardware_pci))
            s.errors[SensorSection.PCI]?.let { Text(it, color = LocalStatusColors.current.bad, style = MaterialTheme.typography.bodySmall) }
            s.pci.forEachIndexed { i, d ->
                if (i > 0) HorizontalDivider()
                PciRow(d)
            }
        }
    }
}

@Composable
private fun PciRow(d: PciDevice) {
    Column(Modifier.padding(vertical = 4.dp)) {
        Text(d.product.ifBlank { d.subclass.ifBlank { d.id } }, style = MaterialTheme.typography.bodyMedium, fontWeight = FontWeight.Bold)
        MutedText(listOf(d.vendor, d.subclass.ifBlank { d.`class` }).filter { it.isNotBlank() }.joinToString("  ·  "))
        Text(
            listOf(d.id, d.driver).filter { it.isNotBlank() }.joinToString("  ·  "),
            style = MaterialTheme.typography.bodySmall,
            fontFamily = FontFamily.Monospace,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}
