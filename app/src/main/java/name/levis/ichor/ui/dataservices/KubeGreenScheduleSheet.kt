package name.levis.ichor.ui.dataservices

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import name.levis.ichor.ui.design.IchorButton
import androidx.compose.material3.ExperimentalMaterial3Api
import name.levis.ichor.ui.design.IchorFilterChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import name.levis.ichor.ui.design.IchorOutlinedButton
import androidx.compose.material3.OutlinedTextField
import name.levis.ichor.ui.design.IchorSwitch
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
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.DataServicesRepository
import name.levis.ichor.model.KubeGreenSleepInfo
import name.levis.ichor.model.kubeGreenDays
import name.levis.ichor.model.kubeGreenTimeValid
import name.levis.ichor.model.kubeGreenWeekdays
import name.levis.ichor.model.kubeGreenWeekdaysValid
import name.levis.ichor.model.kubeGreenZoneValid
import name.levis.ichor.ui.KeyedActions
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.argocd.argoSelfHealer
import name.levis.ichor.ui.components.KubeDenialNote
import name.levis.ichor.ui.components.ResultToasts
import name.levis.ichor.ui.components.rememberKubeCanDenial
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.uiText
import java.time.DayOfWeek
import java.time.ZoneId
import java.time.format.TextStyle
import java.util.Locale

/** A new schedule for a SleepInfo, as KubeGreenSetSchedule takes it ("" wakeUpAt: no wake-up). */
data class KubeGreenSchedule(val weekdays: String, val sleepAt: String, val wakeUpAt: String, val timeZone: String)

/** Outcome of saving the schedule of [label], shown once. */
data class KubeGreenScheduleResult(val label: String, val error: UiText?)

/** Schedule changes, run in [scope] (a ViewModel's); [onChanged] reads the services again after one. */
class KubeGreenActions(
    scope: CoroutineScope,
    private val dataServices: DataServicesRepository,
    private val onChanged: () -> Unit,
) {
    private val actions = KeyedActions<KubeGreenScheduleResult>(scope)
    /** Labels of the SleepInfos with a save in flight. */
    val busy: StateFlow<Set<String>> get() = actions.busy
    val results: Flow<KubeGreenScheduleResult> get() = actions.results
    private val _saved = MutableStateFlow(0)
    /** Bumped after each save that went through: what shows a schedule reads it again. */
    val saved: StateFlow<Int> = _saved.asStateFlow()

    fun setSchedule(info: KubeGreenSleepInfo, schedule: KubeGreenSchedule) {
        val key = info.label
        actions.launch(
            key,
            { dataServices.setKubeGreenSchedule(info.namespace, info.name, schedule.weekdays, schedule.sleepAt, schedule.wakeUpAt, schedule.timeZone) },
            { KubeGreenScheduleResult(key, it.exceptionOrNull()?.uiText()) },
        ) {
            // The next sheet starts from the new schedule, not the one kept for a minute.
            dataServices.forgetKubeGreenNamespace(info.namespace)
            _saved.update { it + 1 }
            onChanged()
        }
    }
}

@Composable
fun KubeGreenScheduleToasts(results: Flow<KubeGreenScheduleResult>) = ResultToasts(results) { context, r ->
    val message = r.error?.resolve(context)?.let { context.getString(R.string.kubegreen_save_failed, r.label, it) }
        ?: context.getString(R.string.kubegreen_saved, r.label)
    message to (r.error != null)
}

/** "Edit schedule", off with the reason when the credentials may not patch the SleepInfo. */
@Composable
fun KubeGreenEditButton(info: KubeGreenSleepInfo, busy: Boolean, onEdit: () -> Unit) {
    val denial = rememberKubeCanDenial("patch", "kube-green.com", "sleepinfos", info.namespace, info.name)
    Column(Modifier.padding(top = 6.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
        IchorOutlinedButton(onClick = onEdit, enabled = !busy && denial == null) { Text(stringResource(R.string.kubegreen_edit_schedule)) }
        KubeDenialNote(denial)
    }
}

/**
 * The SleepInfo's schedule, pre-filled: days (as text when they use cron steps), sleep and wake-up
 * times as "HH:mm" (cron syntax kept), an explicit "No wake-up", and the zone the times are in,
 * always written so a time is never saved without it. Save stays off until something changed
 * and every value is valid; the Argo CD self-heal warning shows when an app would revert it.
 */
@OptIn(ExperimentalMaterial3Api::class, ExperimentalLayoutApi::class)
@Composable
fun KubeGreenScheduleSheet(info: KubeGreenSleepInfo, onDismiss: () -> Unit, onSave: (KubeGreenSchedule) -> Unit) {
    val initialDays = kubeGreenDays(info.weekdays)
    var days by rememberSaveable(info.label) { mutableStateOf(initialDays ?: emptySet()) }
    var weekdaysText by rememberSaveable(info.label) { mutableStateOf(info.weekdays) }
    var sleepAt by rememberSaveable(info.label) { mutableStateOf(info.sleepAt) }
    var noWakeUp by rememberSaveable(info.label) { mutableStateOf(info.wakeUpAt.isEmpty()) }
    var wakeUpAt by rememberSaveable(info.label) { mutableStateOf(info.wakeUpAt.ifEmpty { "08:00" }) }
    // An empty zone is UTC: say so rather than show the phone's zone over UTC times.
    var zone by rememberSaveable(info.label) { mutableStateOf(info.timeZone.ifEmpty { "UTC" }) }
    val phoneZone = remember { ZoneId.systemDefault().id }

    // The days as written stay as written while the same days are picked ("0-6" is not
    // rewritten "*"): nothing to save then, and no change Git would see as drift.
    val weekdays = when {
        initialDays == null -> weekdaysText.trim()
        days == initialDays -> info.weekdays
        else -> kubeGreenWeekdays(days)
    }
    val schedule = KubeGreenSchedule(weekdays, sleepAt.trim(), if (noWakeUp) "" else wakeUpAt.trim(), zone.trim())
    val daysValid = if (initialDays != null) days.isNotEmpty() else kubeGreenWeekdaysValid(weekdays)
    val sleepValid = kubeGreenTimeValid(schedule.sleepAt)
    val wakeValid = noWakeUp || kubeGreenTimeValid(schedule.wakeUpAt)
    val zoneValid = kubeGreenZoneValid(schedule.timeZone)
    val original = KubeGreenSchedule(info.weekdays, info.sleepAt, info.wakeUpAt, info.timeZone.ifEmpty { "UTC" })
    val canSave = daysValid && sleepValid && wakeValid && zoneValid && schedule != original

    val app = LocalContext.current.applicationContext as? TalosApp
    val healer = remember(info.label) { app?.gitOpsRepository?.argoSelfHealer("SleepInfo", info.namespace, info.name) }

    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(
            Modifier.verticalScroll(rememberScrollState()).padding(horizontal = 16.dp).navigationBarsPadding().padding(bottom = 16.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            Text(stringResource(R.string.kubegreen_schedule_title, info.label), style = MaterialTheme.typography.titleMedium)

            Text(stringResource(R.string.kubegreen_weekdays), style = MaterialTheme.typography.labelLarge)
            if (initialDays != null) {
                FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                    // Monday first, as the week reads; cron numbers Sunday 0.
                    listOf(1, 2, 3, 4, 5, 6, 0).forEach { d ->
                        val name = DayOfWeek.of(if (d == 0) 7 else d).getDisplayName(TextStyle.SHORT, Locale.getDefault())
                        IchorFilterChip(selected = d in days, onClick = { days = if (d in days) days - d else days + d }, label = { Text(name) })
                    }
                }
                if (!daysValid) ErrorLine(stringResource(R.string.kubegreen_pick_day))
            } else {
                OutlinedTextField(
                    weekdaysText, { weekdaysText = it }, Modifier.fillMaxWidth(),
                    label = { Text(stringResource(R.string.kubegreen_weekdays_cron)) },
                    singleLine = true, isError = !daysValid, textStyle = MaterialTheme.typography.bodyMedium.copy(fontFamily = FontFamily.Monospace),
                )
            }

            TimeField(stringResource(R.string.kubegreen_sleep_at), sleepAt, sleepValid) { sleepAt = it }
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                IchorSwitch(checked = noWakeUp, onCheckedChange = { noWakeUp = it })
                Text(stringResource(R.string.kubegreen_no_wake_up), style = MaterialTheme.typography.bodyMedium, modifier = Modifier.weight(1f))
            }
            if (!noWakeUp) TimeField(stringResource(R.string.kubegreen_wake_up_at), wakeUpAt, wakeValid) { wakeUpAt = it }

            OutlinedTextField(
                zone, { zone = it }, Modifier.fillMaxWidth(),
                label = { Text(stringResource(R.string.kubegreen_time_zone)) },
                singleLine = true, isError = !zoneValid,
                supportingText = if (!zoneValid) ({ Text(stringResource(R.string.kubegreen_zone_invalid)) }) else null,
            )
            if (zone.trim() != phoneZone) {
                IchorTextButton(onClick = { zone = phoneZone }) { Text(stringResource(R.string.kubegreen_use_phone_zone, phoneZone)) }
            }

            healer?.let { Text(stringResource(R.string.argo_revert_text_change, it.app.name), color = LocalStatusColors.current.warn) }

            IchorButton(onClick = { onSave(schedule) }, enabled = canSave, modifier = Modifier.fillMaxWidth()) {
                Text(stringResource(R.string.kubegreen_save))
            }
        }
    }
}

@Composable
private fun TimeField(label: String, value: String, valid: Boolean, onChange: (String) -> Unit) {
    OutlinedTextField(
        value, onChange, Modifier.fillMaxWidth(),
        label = { Text(label) },
        singleLine = true,
        isError = !valid,
        supportingText = if (!valid) ({ Text(stringResource(R.string.kubegreen_time_invalid)) }) else null,
        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Ascii),
        textStyle = MaterialTheme.typography.bodyMedium.copy(fontFamily = FontFamily.Monospace),
    )
}

@Composable
private fun ErrorLine(text: String) {
    Text(text, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.error)
}
