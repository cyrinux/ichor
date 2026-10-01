package name.levis.ichor.ui.events

import android.text.format.DateUtils
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.ErrorOutline
import androidx.compose.material.icons.outlined.Flag
import androidx.compose.material.icons.outlined.Info
import androidx.compose.material.icons.outlined.Lan
import androidx.compose.material.icons.outlined.LinearScale
import androidx.compose.material.icons.outlined.Memory
import androidx.compose.material.icons.outlined.MiscellaneousServices
import androidx.compose.material.icons.outlined.RestartAlt
import androidx.compose.material.icons.outlined.TaskAlt
import androidx.compose.material.icons.outlined.Tune
import androidx.compose.material.icons.outlined.Warning
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.EventRow
import name.levis.ichor.ui.components.agoLabel
import name.levis.ichor.ui.theme.LocalStatusColors
import java.text.DateFormat
import java.util.Date

private fun kindIcon(kind: String): Pair<ImageVector, Int> = when (kind) {
    "service" -> Icons.Outlined.MiscellaneousServices to R.string.events_kind_service
    "sequence" -> Icons.Outlined.LinearScale to R.string.events_kind_sequence
    "phase" -> Icons.Outlined.Flag to R.string.events_kind_phase
    "task" -> Icons.Outlined.TaskAlt to R.string.events_kind_task
    "machine" -> Icons.Outlined.Memory to R.string.events_kind_machine
    "config" -> Icons.Outlined.Tune to R.string.events_kind_config
    "address" -> Icons.Outlined.Lan to R.string.events_kind_address
    "restart" -> Icons.Outlined.RestartAlt to R.string.events_kind_restart
    else -> Icons.Outlined.Info to R.string.events_kind_other
}

/** Time of day for today's events, date and time for older ones. */
private fun absoluteTime(at: Long): String =
    if (DateUtils.isToday(at)) DateFormat.getTimeInstance(DateFormat.MEDIUM).format(Date(at))
    else DateFormat.getDateTimeInstance(DateFormat.SHORT, DateFormat.MEDIUM).format(Date(at))

@Composable
fun EventItem(row: EventRow, hostname: String, showNode: Boolean, now: Long) {
    val e = row.event
    val colors = LocalStatusColors.current
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    val severityColor = when (e.severity) {
        "error" -> colors.bad
        "warning" -> colors.warn
        else -> null
    }
    val (icon, kindLabel) = kindIcon(e.kind)
    Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), verticalAlignment = Alignment.Top) {
        Icon(
            icon,
            contentDescription = stringResource(kindLabel),
            tint = severityColor ?: muted,
            modifier = Modifier.size(20.dp).padding(top = 2.dp),
        )
        Column(Modifier.weight(1f).padding(start = 12.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                when (e.severity) {
                    "error" -> SeverityIcon(Icons.Outlined.ErrorOutline, stringResource(R.string.events_severity_error), colors.bad)
                    "warning" -> SeverityIcon(Icons.Outlined.Warning, stringResource(R.string.events_severity_warning), colors.warn)
                }
                Text(
                    listOf(e.subject, e.action).filter { it.isNotBlank() }.joinToString(" · ").ifEmpty { stringResource(kindLabel) },
                    style = MaterialTheme.typography.bodyMedium,
                    fontWeight = FontWeight.Bold,
                    modifier = Modifier.weight(1f),
                )
                if (row.count > 1) {
                    val repeated = pluralStringResource(R.plurals.events_repeated, row.count, row.count)
                    Text(
                        "×${row.count}",
                        style = MaterialTheme.typography.labelMedium,
                        color = muted,
                        modifier = Modifier.padding(start = 8.dp).semantics { contentDescription = repeated },
                    )
                }
            }
            if (e.message.isNotBlank()) {
                Text(e.message, style = MaterialTheme.typography.bodySmall, color = severityColor ?: MaterialTheme.colorScheme.onSurface)
            }
            val time = if (e.at > 0) "${absoluteTime(e.at)} · ${agoLabel(now - e.at)}" else ""
            Text(
                listOf(time, if (showNode) hostname else "").filter { it.isNotEmpty() }.joinToString(" · "),
                style = MaterialTheme.typography.labelSmall,
                color = muted,
            )
        }
    }
}

@Composable
private fun SeverityIcon(icon: ImageVector, description: String, tint: androidx.compose.ui.graphics.Color) {
    Icon(icon, contentDescription = description, tint = tint, modifier = Modifier.size(16.dp).padding(end = 4.dp))
}
