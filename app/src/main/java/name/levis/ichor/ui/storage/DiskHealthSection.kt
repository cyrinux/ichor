package name.levis.ichor.ui.storage

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.HorizontalDivider
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
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.DiskHealth
import name.levis.ichor.model.DiskHealthReport
import name.levis.ichor.model.DiskVerdict
import name.levis.ichor.model.verdict
import name.levis.ichor.ui.components.InfoNotice
import name.levis.ichor.ui.components.InfoRow
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.theme.LocalStatusColors

@Composable
fun DiskHealthContent(report: DiskHealthReport) {
    when {
        !report.supported -> UnsupportedReason(report.reason)
        report.disks.isEmpty() -> InfoNotice(report.reason.ifBlank { stringResource(R.string.storage_health_empty) })
        else -> {
            // Supported but e.g. SMART collection is not configured: the core says why.
            if (report.reason.isNotBlank()) InfoNotice(report.reason)
            report.disks.forEachIndexed { i, disk ->
                if (i > 0) HorizontalDivider()
                DiskHealthCard(disk)
            }
        }
    }
}

@Composable
private fun DiskHealthCard(disk: DiskHealth) {
    val colors = LocalStatusColors.current
    var expanded by rememberSaveable(disk.device) { mutableStateOf(false) }
    Column {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Column(Modifier.weight(1f)) {
                Text(disk.device, style = MaterialTheme.typography.titleSmall, fontFamily = FontFamily.Monospace)
                if (disk.model.isNotBlank()) {
                    MutedText(disk.model)
                }
            }
            when (disk.verdict) {
                DiskVerdict.HEALTHY -> StatusPill(stringResource(R.string.storage_health_healthy), colors.ok)
                DiskVerdict.FAILING -> StatusPill(stringResource(R.string.storage_health_failing), colors.bad)
                DiskVerdict.UNKNOWN -> StatusPill(stringResource(R.string.storage_health_unknown), colors.muted)
            }
        }
        if (disk.message.isNotBlank()) {
            MutedText(disk.message)
        }
        if (disk.serial.isNotBlank()) InfoRow(stringResource(R.string.storage_health_serial), disk.serial, mono = true)
        disk.temperatureC?.let {
            InfoRow(stringResource(R.string.storage_health_temperature), "$it °C")
        }
        disk.powerOnHours?.let {
            InfoRow(stringResource(R.string.storage_health_power_on), stringResource(R.string.storage_health_hours, it))
        }
        disk.wearPercent?.let {
            InfoRow(stringResource(R.string.storage_health_wear), "$it %")
        }
        disk.criticalWarnings.forEach { warning ->
            Text(warning, color = colors.bad, style = MaterialTheme.typography.bodySmall, modifier = Modifier.padding(top = 2.dp))
        }
        if (disk.attributes.isNotEmpty()) {
            TextButton(onClick = { expanded = !expanded }) {
                Text(
                    if (expanded) {
                        stringResource(R.string.storage_health_hide_attributes)
                    } else {
                        stringResource(R.string.storage_health_show_attributes, disk.attributes.size)
                    },
                )
            }
            if (expanded) disk.attributes.forEach { InfoRow(it.k, it.v, mono = true) }
        }
    }
}
