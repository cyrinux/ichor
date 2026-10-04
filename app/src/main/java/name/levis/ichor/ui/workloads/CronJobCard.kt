package name.levis.ichor.ui.workloads

import android.text.format.DateUtils
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Lock
import androidx.compose.material.icons.outlined.PauseCircle
import androidx.compose.material.icons.outlined.PlayArrow
import androidx.compose.material.icons.outlined.PlayCircle
import androidx.compose.material.icons.outlined.Schedule
import androidx.compose.material.icons.outlined.TouchApp
import androidx.compose.material.icons.outlined.Update
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.JobRunState
import name.levis.ichor.model.KubeCronJob
import name.levis.ichor.model.KubeJobRun
import name.levis.ichor.ui.apps.AppIconTile
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.localizedDuration
import name.levis.ichor.ui.theme.LocalStatusColors

/** One CronJob: who it is, when it runs, how its last runs went, Suspend/Resume and Run now. Tap for the runs. */
@Composable
internal fun CronJobCard(
    cronJob: KubeCronJob,
    showNamespace: Boolean,
    expanded: Boolean,
    triggering: Boolean,
    suspending: Boolean,
    onToggle: () -> Unit,
    onRun: () -> Unit,
    onSuspend: () -> Unit,
) {
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    Card(
        onClick = onToggle,
        shape = RoundedCornerShape(20.dp),
        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceContainerLow),
        modifier = Modifier.fillMaxWidth(),
    ) {
        Column(Modifier.padding(14.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            CronJobHeader(cronJob, showNamespace)
            if (cronJob.description.isNotBlank()) {
                Text(cronJob.description, style = MaterialTheme.typography.bodySmall, color = muted)
            }
            CronJobTiming(cronJob)
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                    RunHistoryStrip(cronJob.runs)
                    LastRunText(cronJob.runs.firstOrNull())
                }
                SuspendButton(cronJob, suspending, onSuspend)
                RunNowButton(cronJob, triggering, onRun)
            }
            AnimatedVisibility(expanded) { RunList(cronJob.runs) }
        }
    }
}

@Composable
private fun CronJobHeader(cronJob: KubeCronJob, showNamespace: Boolean) {
    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
        AppIconTile(cronJob.iconApp, size = 48.dp, fallback = { DefaultCronIcon() })
        Column(Modifier.weight(1f)) {
            Text(
                cronJob.displayName,
                style = MaterialTheme.typography.titleMedium,
                fontWeight = FontWeight.SemiBold,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            val subtitle = listOfNotNull(cronJob.name.takeIf { cronJob.title.isNotBlank() }, cronJob.namespace.takeIf { showNamespace })
            if (subtitle.isNotEmpty()) {
                Text(
                    subtitle.joinToString("  ·  "),
                    style = MaterialTheme.typography.labelSmall,
                    fontFamily = FontFamily.Monospace,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }
        StatusPill(stringResource(cronJob.runState.label), cronJob.runState.color())
    }
}

/** The clock drawn when the CronJob names no icon and its image says nothing. */
@Composable
private fun DefaultCronIcon() {
    Box(Modifier.fillMaxSize().background(MaterialTheme.colorScheme.primaryContainer), contentAlignment = Alignment.Center) {
        Icon(Icons.Outlined.Schedule, contentDescription = null, tint = MaterialTheme.colorScheme.onPrimaryContainer, modifier = Modifier.size(26.dp))
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun CronJobTiming(cronJob: KubeCronJob) {
    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
        val schedule = listOf(cronJob.schedule, cronJob.timeZone).filter { it.isNotBlank() }.joinToString("  ·  ")
        MetaChip(Icons.Outlined.Schedule, schedule, mono = true)
        when {
            cronJob.suspended -> MetaChip(Icons.Outlined.PauseCircle, stringResource(R.string.cronjobs_suspended), LocalStatusColors.current.warn)
            // Not from cached data that has gone by.
            cronJob.nextRun > System.currentTimeMillis() -> {
                val next = DateUtils.getRelativeTimeSpanString(cronJob.nextRun, System.currentTimeMillis(), DateUtils.MINUTE_IN_MILLIS)
                MetaChip(Icons.Outlined.Update, stringResource(R.string.cronjobs_next_run, next))
            }
        }
    }
}

@Composable
private fun MetaChip(icon: ImageVector, text: String, color: Color = MaterialTheme.colorScheme.onSurfaceVariant, mono: Boolean = false) {
    Row(
        Modifier.background(MaterialTheme.colorScheme.surfaceContainerHighest, RoundedCornerShape(8.dp)).padding(horizontal = 8.dp, vertical = 4.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(5.dp),
    ) {
        Icon(icon, contentDescription = null, tint = color, modifier = Modifier.size(14.dp))
        Text(text, style = MaterialTheme.typography.labelMedium, color = color, fontFamily = if (mono) FontFamily.Monospace else null)
    }
}

/** The recent runs as small bars, oldest left; a manual run is drawn hollow. */
@Composable
private fun RunHistoryStrip(runs: List<KubeJobRun>) {
    if (runs.isEmpty()) return
    Row(horizontalArrangement = Arrangement.spacedBy(4.dp)) {
        runs.reversed().forEach { run ->
            val color = run.runState.color()
            val shape = RoundedCornerShape(3.dp)
            val bar = Modifier.width(14.dp).height(8.dp)
            Box(if (run.manual) bar.border(1.5.dp, color, shape) else bar.background(color, shape))
        }
    }
}

@Composable
private fun LastRunText(run: KubeJobRun?) {
    val text = run?.let { r ->
        val ago = DateUtils.getRelativeTimeSpanString(r.started, System.currentTimeMillis(), DateUtils.MINUTE_IN_MILLIS)
        val duration = r.durationMillis?.let { localizedDuration(it / 1000) }
        listOfNotNull(stringResource(R.string.cronjobs_last_run, ago), duration).joinToString("  ·  ")
    } ?: stringResource(R.string.cronjobs_no_runs)
    Text(text, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
}

/** Pauses the schedule, or resumes it when suspended. */
@Composable
private fun SuspendButton(cronJob: KubeCronJob, suspending: Boolean, onSuspend: () -> Unit) {
    if (suspending) {
        CircularProgressIndicator(Modifier.padding(12.dp).size(24.dp), strokeWidth = 2.dp)
        return
    }
    TooltipIconButton(
        if (cronJob.suspended) Icons.Outlined.PlayCircle else Icons.Outlined.PauseCircle,
        stringResource(if (cronJob.suspended) R.string.cronjobs_resume else R.string.cronjobs_suspend),
        onClick = onSuspend,
    )
}

@Composable
private fun RunNowButton(cronJob: KubeCronJob, triggering: Boolean, onRun: () -> Unit) {
    when {
        triggering -> CircularProgressIndicator(Modifier.padding(horizontal = 24.dp).size(24.dp), strokeWidth = 2.dp)
        cronJob.triggerable -> FilledTonalButton(onClick = onRun, contentPadding = ButtonDefaults.ButtonWithIconContentPadding) {
            Icon(Icons.Outlined.PlayArrow, contentDescription = null, modifier = Modifier.size(ButtonDefaults.IconSize))
            Spacer(Modifier.size(ButtonDefaults.IconSpacing))
            Text(stringResource(R.string.cronjobs_run_now))
        }
        else -> Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(4.dp)) {
            val muted = MaterialTheme.colorScheme.onSurfaceVariant
            Icon(Icons.Outlined.Lock, contentDescription = null, tint = muted, modifier = Modifier.size(16.dp))
            Text(stringResource(R.string.cronjobs_schedule_only), style = MaterialTheme.typography.labelMedium, color = muted)
        }
    }
}

@Composable
private fun RunList(runs: List<KubeJobRun>) {
    Column {
        HorizontalDivider(Modifier.padding(bottom = 6.dp))
        Text(stringResource(R.string.cronjobs_runs), style = MaterialTheme.typography.labelLarge, modifier = Modifier.padding(bottom = 4.dp))
        if (runs.isEmpty()) {
            Text(stringResource(R.string.cronjobs_no_runs), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        runs.forEach { RunRow(it) }
    }
}

@Composable
private fun RunRow(run: KubeJobRun) {
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    Row(Modifier.fillMaxWidth().padding(vertical = 4.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        Box(Modifier.size(8.dp).background(run.runState.color(), CircleShape))
        Column(Modifier.weight(1f)) {
            Text(run.name, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis)
            val ago = DateUtils.getRelativeTimeSpanString(run.started, System.currentTimeMillis(), DateUtils.MINUTE_IN_MILLIS)
            val parts = listOfNotNull(stringResource(run.runState.label), ago.toString(), run.durationMillis?.let { localizedDuration(it / 1000) })
            Text(parts.joinToString("  ·  "), style = MaterialTheme.typography.labelSmall, color = muted)
        }
        if (run.manual) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(3.dp)) {
                Icon(Icons.Outlined.TouchApp, contentDescription = null, tint = muted, modifier = Modifier.size(14.dp))
                Text(stringResource(R.string.cronjobs_manual), style = MaterialTheme.typography.labelSmall, color = muted)
            }
        }
    }
}

private val JobRunState.label: Int
    get() = when (this) {
        JobRunState.RUNNING -> R.string.cronjobs_state_running
        JobRunState.SUCCEEDED -> R.string.cronjobs_state_succeeded
        JobRunState.FAILED -> R.string.cronjobs_state_failed
        JobRunState.NEVER -> R.string.cronjobs_state_never
    }

@Composable
private fun JobRunState.color(): Color {
    val colors = LocalStatusColors.current
    return when (this) {
        JobRunState.RUNNING -> MaterialTheme.colorScheme.primary
        JobRunState.SUCCEEDED -> colors.ok
        JobRunState.FAILED -> colors.bad
        JobRunState.NEVER -> colors.muted
    }
}
