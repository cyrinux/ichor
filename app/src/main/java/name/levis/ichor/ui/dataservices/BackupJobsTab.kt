package name.levis.ichor.ui.dataservices

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Schedule
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.BackupCronJob
import name.levis.ichor.model.BackupJobStatus
import name.levis.ichor.model.CronShape
import name.levis.ichor.model.ServiceHealth
import name.levis.ichor.model.describeCron
import name.levis.ichor.ui.components.CardListPadding
import name.levis.ichor.ui.components.EmptyState
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.ResourceRow
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.components.Tag
import name.levis.ichor.ui.components.cardRows
import name.levis.ichor.ui.components.color
import name.levis.ichor.util.timeAgo
import java.time.format.TextStyle
import java.util.Locale

/** The backup CronJobs, failing first: their last success, failure streak and schedule. */
@Composable
fun BackupJobsTab(status: BackupJobStatus) {
    val now = System.currentTimeMillis()
    LazyColumn(Modifier.fillMaxSize(), contentPadding = CardListPadding) {
        items(listOf(status.error, status.jobsError).filter { it.isNotEmpty() }) {
            InlineError(stringResource(R.string.data_services_unreadable, it), Modifier.padding(16.dp))
        }
        if (status.jobs.isEmpty() && status.error.isEmpty()) {
            item { EmptyState(Icons.Outlined.Schedule, stringResource(R.string.backup_empty)) }
        }
        cardRows(status.jobs, key = { it.label }) { BackupJobRow(it, now) }
    }
}

@Composable
private fun BackupJobRow(j: BackupCronJob, now: Long) {
    val subtitle = if (j.lastSuccess > 0) stringResource(R.string.backup_last_success, timeAgo(j.lastSuccess, now)) else stringResource(R.string.backup_never_succeeded)
    ResourceRow(
        j.rowHealth(now),
        j.label,
        Modifier.padding(horizontal = 16.dp),
        subtitle = subtitle,
        trailing = {
            if (j.failureStreak > 0) {
                StatusPill(pluralStringResource(R.plurals.backup_failures_in_row, j.failureStreak, j.failureStreak), ServiceHealth.CRITICAL.color())
            }
        },
    ) {
        Row(Modifier.padding(top = 4.dp), horizontalArrangement = Arrangement.spacedBy(4.dp)) {
            if (j.schedule.isNotEmpty()) {
                val spoken = cronText(j.schedule)
                Tag(j.schedule, mono = true, modifier = Modifier.clearAndSetSemantics { contentDescription = spoken })
            }
            if (j.suspended) Tag(stringResource(R.string.backup_suspended))
        }
    }
}

/** "weekly on Monday at 09:00" for the common shapes, the schedule as written otherwise. */
@Composable
private fun cronText(schedule: String): String {
    val shape = describeCron(schedule) ?: return schedule
    val locale = LocalConfiguration.current.locales[0] ?: Locale.getDefault()
    val time = String.format(Locale.ROOT, "%02d:%02d", shape.hour, shape.minute)
    return when (shape) {
        is CronShape.Daily -> stringResource(R.string.cron_daily, time)
        is CronShape.Weekly -> stringResource(R.string.cron_weekly, shape.day.getDisplayName(TextStyle.FULL, locale), time)
        is CronShape.Monthly -> stringResource(R.string.cron_monthly, shape.dayOfMonth, time)
    }
}
