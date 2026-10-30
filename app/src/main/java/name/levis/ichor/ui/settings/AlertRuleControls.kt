package name.levis.ichor.ui.settings

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import name.levis.ichor.ui.design.IchorOutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import name.levis.ichor.ui.design.IchorTextButton
import androidx.compose.material3.TimePicker
import androidx.compose.material3.rememberTimePickerState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.ALERT_SEVERITY_ALL
import name.levis.ichor.model.ALERT_SEVERITY_CRITICAL
import name.levis.ichor.model.ALERT_SEVERITY_WARNING
import name.levis.ichor.model.QuietHours
import name.levis.ichor.ui.components.MutedText
import java.time.ZoneId
import java.util.TimeZone

/** The minimum severities, lowest first, as the rules store them. */
val ALERT_RULE_SEVERITIES = listOf(ALERT_SEVERITY_ALL, ALERT_SEVERITY_WARNING, ALERT_SEVERITY_CRITICAL)

@Composable
fun severityLabel(severity: String): String = stringResource(
    when (severity) {
        ALERT_SEVERITY_WARNING -> R.string.alert_rules_severity_warning
        ALERT_SEVERITY_CRITICAL -> R.string.alert_rules_severity_critical
        else -> R.string.alert_rules_severity_all
    },
)

/** "Off" for 0, else "15 min": a repeat choice. */
@Composable
fun repeatLabel(minutes: Int): String =
    if (minutes <= 0) stringResource(R.string.alert_rules_off) else stringResource(R.string.alert_repeat_minutes, minutes)

/** A segmented choice among [labels], [selected] its index, under a [title] that also labels it for a screen reader. */
@Composable
fun RuleSegments(title: String, labels: List<String>, selected: Int, enabled: Boolean, onSelect: (Int) -> Unit) {
    Text(title, style = MaterialTheme.typography.labelLarge)
    SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth().semantics { contentDescription = title }) {
        labels.forEachIndexed { index, label ->
            SegmentedButton(
                selected = index == selected,
                onClick = { onSelect(index) },
                enabled = enabled,
                shape = SegmentedButtonDefaults.itemShape(index, labels.size),
            ) { Text(label, maxLines = 1, overflow = TextOverflow.Ellipsis) }
        }
    }
}

/** A title, its hint and a switch on one row. */
@Composable
fun RuleSwitch(title: String, hint: String?, checked: Boolean, enabled: Boolean, onChange: (Boolean) -> Unit) {
    Row(verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f)) {
            Text(title, style = MaterialTheme.typography.titleSmall)
            hint?.let { MutedText(it) }
        }
        Switch(checked = checked, onCheckedChange = onChange, enabled = enabled, modifier = Modifier.padding(start = 12.dp))
    }
}

/**
 * A quiet-hours window: its start and end (a time picker each), its zone ("" the phone's, a
 * list to pick another from) and "except critical".
 */
@Composable
fun QuietHoursEditor(quiet: QuietHours, enabled: Boolean, onChange: (QuietHours) -> Unit) {
    var picking by remember { mutableStateOf<Boolean?>(null) } // true: start, false: end
    var zones by remember { mutableStateOf(false) }
    Row(horizontalArrangement = Arrangement.spacedBy(8.dp), modifier = Modifier.fillMaxWidth()) {
        IchorOutlinedButton(onClick = { picking = true }, enabled = enabled, modifier = Modifier.weight(1f)) {
            Text(stringResource(R.string.alert_rules_from, quiet.start), maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
        IchorOutlinedButton(onClick = { picking = false }, enabled = enabled, modifier = Modifier.weight(1f)) {
            Text(stringResource(R.string.alert_rules_until, quiet.end), maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
    }
    IchorOutlinedButton(onClick = { zones = true }, enabled = enabled, modifier = Modifier.fillMaxWidth()) {
        Text(stringResource(R.string.alert_rules_zone, zoneLabel(quiet.tz)), maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
    RuleSwitch(
        stringResource(R.string.alert_rules_except_critical),
        stringResource(R.string.alert_rules_except_critical_desc),
        quiet.exceptCritical,
        enabled,
    ) { onChange(quiet.copy(exceptCritical = it)) }

    picking?.let { start ->
        ClockDialog(
            title = stringResource(if (start) R.string.alert_rules_pick_start else R.string.alert_rules_pick_end),
            value = if (start) quiet.start else quiet.end,
            onPick = { onChange(if (start) quiet.copy(start = it) else quiet.copy(end = it)) },
            onDismiss = { picking = null },
        )
    }
    if (zones) ZoneDialog(quiet.tz, onPick = { onChange(quiet.copy(tz = it)) }, onDismiss = { zones = false })
}

/** "Phone's (Europe/Paris)" for "", else the zone itself. */
@Composable
private fun zoneLabel(zone: String): String =
    if (zone.isBlank()) stringResource(R.string.alert_rules_zone_phone, TimeZone.getDefault().id) else zone

/** Parses "HH:MM" into (hour, minute), 0:00 when it is not one. */
internal fun clockOf(value: String): Pair<Int, Int> {
    val hour = value.substringBefore(':').toIntOrNull()?.takeIf { it in 0..23 } ?: return 0 to 0
    val minute = value.substringAfter(':', "").toIntOrNull()?.takeIf { it in 0..59 } ?: return 0 to 0
    return hour to minute
}

/** "07:05", as the rules store it. */
internal fun clockText(hour: Int, minute: Int): String = "%02d:%02d".format(java.util.Locale.ROOT, hour, minute)

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun ClockDialog(title: String, value: String, onPick: (String) -> Unit, onDismiss: () -> Unit) {
    val (hour, minute) = clockOf(value)
    val is24 = android.text.format.DateFormat.is24HourFormat(LocalContext.current)
    val state = rememberTimePickerState(initialHour = hour, initialMinute = minute, is24Hour = is24)
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(title) },
        text = { TimePicker(state = state, modifier = Modifier.semantics { contentDescription = title }) },
        confirmButton = {
            IchorTextButton(onClick = {
                onPick(clockText(state.hour, state.minute))
                onDismiss()
            }) { Text(stringResource(R.string.common_ok)) }
        },
        dismissButton = { IchorTextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}

/** The phone's zone first ("", followed when the phone moves), then every region zone, filtered by what is typed. */
@Composable
private fun ZoneDialog(current: String, onPick: (String) -> Unit, onDismiss: () -> Unit) {
    var query by rememberSaveable { mutableStateOf("") }
    val all = remember { ZoneId.getAvailableZoneIds().filter { '/' in it && !it.startsWith("Etc/") && !it.startsWith("SystemV/") }.sorted() }
    val shown = remember(query) { listOf("") + all.filter { it.contains(query.trim(), ignoreCase = true) } }
    val search = stringResource(R.string.alert_rules_zone_search)
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.alert_rules_zone_title)) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedTextField(
                    value = query,
                    onValueChange = { query = it },
                    label = { Text(search) },
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth(),
                )
                LazyColumn(Modifier.heightIn(max = 360.dp)) {
                    items(shown, key = { it.ifEmpty { "phone" } }) { zone ->
                        val label = zoneLabel(zone)
                        Text(
                            label,
                            style = MaterialTheme.typography.bodyLarge,
                            color = if (zone == current) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurface,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                            modifier = Modifier
                                .fillMaxWidth()
                                .clickable {
                                    onPick(zone)
                                    onDismiss()
                                }
                                .padding(vertical = 10.dp),
                        )
                    }
                }
            }
        },
        confirmButton = { IchorTextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}
