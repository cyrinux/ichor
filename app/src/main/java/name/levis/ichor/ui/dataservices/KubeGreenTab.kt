package name.levis.ichor.ui.dataservices

import androidx.compose.animation.animateContentSize
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.ExpandLess
import androidx.compose.material.icons.outlined.ExpandMore
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import name.levis.ichor.R
import name.levis.ichor.model.KubeGreenReason
import name.levis.ichor.model.KubeGreenSleepInfo
import name.levis.ichor.model.KubeGreenStatus
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.InfoRow
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.util.formatDateTime
import name.levis.ichor.util.timeAgo

/**
 * kube-green's sleep schedules, problems first: one row per SleepInfo with whether its
 * namespace is asleep now and when it wakes up (or sleeps again), opening to the details and
 * "Edit schedule".
 */
@Composable
fun KubeGreenTab(status: KubeGreenStatus, actions: KubeGreenActions) {
    var expanded by rememberSaveable { mutableStateOf(setOf<String>()) }
    var editing by rememberSaveable { mutableStateOf<String?>(null) }
    val busy by actions.busy.collectAsStateWithLifecycle()

    status.sleepInfos.firstOrNull { it.label == editing }?.let { info ->
        KubeGreenScheduleSheet(info, onDismiss = { editing = null }, onSave = { schedule ->
            editing = null
            actions.setSchedule(info, schedule)
        })
    }

    LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 16.dp)) {
        if (status.error.isNotEmpty()) item { InlineError(stringResource(R.string.data_services_unreadable, status.error), Modifier.padding(16.dp)) }
        if (status.sleepInfos.isEmpty()) item(key = "empty") { EmptyText(stringResource(R.string.kubegreen_empty)) }
        items(status.sleepInfos, key = { it.label }) { s ->
            SleepInfoRow(s, open = s.label in expanded, onToggle = { expanded = if (s.label in expanded) expanded - s.label else expanded + s.label }) {
                Details(s)
                Column(Modifier.padding(start = 22.dp)) {
                    KubeGreenEditButton(s, busy = s.label in busy, onEdit = { editing = s.label })
                }
            }
            HorizontalDivider()
        }
    }
}

@Composable
private fun SleepInfoRow(s: KubeGreenSleepInfo, open: Boolean, onToggle: () -> Unit, details: @Composable () -> Unit) {
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    Column(Modifier.fillMaxWidth().clickable(onClick = onToggle).animateContentSize().padding(horizontal = 16.dp, vertical = 8.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            HealthDot(s.serviceHealth)
            Spacer(Modifier.size(12.dp))
            Column(Modifier.weight(1f)) {
                Text(s.label, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis)
                Text(
                    stateLine(s),
                    style = MaterialTheme.typography.labelSmall,
                    color = s.serviceHealth.color().takeIf { s.serviceHealth.needsAttention } ?: muted,
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                )
            }
            Icon(if (open) Icons.Outlined.ExpandLess else Icons.Outlined.ExpandMore, contentDescription = null, tint = muted)
        }
        if (open) details()
    }
}

/** "Asleep · wakes up 15 Oct, 07:00", "Awake · sleeps 14 Oct, 20:00", "Asleep · until changed by hand". */
@Composable
private fun stateLine(s: KubeGreenSleepInfo): String {
    val state = stringResource(if (s.asleep) R.string.kubegreen_state_asleep else R.string.kubegreen_state_awake)
    val next = when {
        s.asleep && s.nextWakeUpAt > 0 -> stringResource(R.string.kubegreen_wakes, formatDateTime(s.nextWakeUpAt))
        s.asleep -> stringResource(R.string.kubegreen_until_changed)
        s.nextSleepAt > 0 -> stringResource(R.string.kubegreen_sleeps, formatDateTime(s.nextSleepAt))
        else -> null
    }

    return listOfNotNull(state, next, s.reasonList.map { reasonText(it) }.takeIf { it.isNotEmpty() }?.joinToString(", ")).joinToString(" · ")
}

@Composable
private fun Details(s: KubeGreenSleepInfo) {
    Column(Modifier.padding(start = 22.dp, top = 8.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
        InfoRow(stringResource(R.string.kubegreen_weekdays), weekdayNames(s.weekdays))
        InfoRow(stringResource(R.string.kubegreen_schedule), "${s.sleepAt} – ${s.wakeUpAt.ifEmpty { "…" }}", mono = true)
        if (s.timeZone.isNotEmpty()) InfoRow(stringResource(R.string.kubegreen_time_zone), s.timeZone, mono = true)
        InfoRow(
            stringResource(R.string.kubegreen_suspends),
            listOfNotNull(
                stringResource(R.string.kubegreen_deployments).takeIf { s.suspendDeployments },
                stringResource(R.string.kubegreen_statefulsets).takeIf { s.suspendStatefulSets },
                stringResource(R.string.kubegreen_cronjobs).takeIf { s.suspendCronJobs },
            ).joinToString(", ").ifEmpty { stringResource(R.string.kubegreen_nothing) },
        )
        val counts = listOfNotNull(
            stringResource(R.string.kubegreen_excluded, s.excludeCount).takeIf { s.excludeCount > 0 },
            stringResource(R.string.kubegreen_included, s.includeCount).takeIf { s.includeCount > 0 },
            stringResource(R.string.kubegreen_patches, s.patchCount).takeIf { s.patchCount > 0 },
        ).joinToString(", ")
        if (counts.isNotEmpty()) InfoRow(stringResource(R.string.kubegreen_scope), counts)
        if (s.lastScheduleAt > 0) InfoRow(stringResource(R.string.kubegreen_last_run), timeAgo(s.lastScheduleAt))
    }
}

/** "Mon – Fri" from the cron day-of-week field ("1-5", "*", "1,3,5"). */
private fun weekdayNames(weekdays: String): String {
    if (weekdays.isEmpty() || weekdays == "*") return "–"
    val names = listOf("Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat")

    return weekdays.split(",").joinToString(", ") { part ->
        val (a, b) = part.split("-").map { it.trim().toIntOrNull() }.let { it.getOrNull(0) to it.getOrNull(1) }
        when {
            a == null -> part
            b == null -> names[a % 7]
            else -> "${names[a % 7]} – ${names[b % 7]}"
        }
    }
}

@Composable
private fun reasonText(reason: KubeGreenReason): String = when (reason) {
    KubeGreenReason.NO_WAKE_UP -> stringResource(R.string.kubegreen_reason_no_wake_up)
    KubeGreenReason.INVALID_SCHEDULE -> stringResource(R.string.kubegreen_reason_invalid_schedule)
    KubeGreenReason.OVERDUE -> stringResource(R.string.kubegreen_reason_overdue)
    KubeGreenReason.NEVER_RAN -> stringResource(R.string.kubegreen_reason_never_ran)
}
