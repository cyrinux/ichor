package name.levis.talosmobile.ui.upgrade

import androidx.annotation.StringRes
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.Error
import androidx.compose.material.icons.outlined.RadioButtonUnchecked
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import name.levis.talosmobile.R
import name.levis.talosmobile.data.UpgradeRunState
import name.levis.talosmobile.model.StepStatus
import name.levis.talosmobile.model.TimelineStep
import name.levis.talosmobile.model.UpgradePhase
import name.levis.talosmobile.model.upgradeTimeline
import name.levis.talosmobile.ui.components.KeepScreenOn
import name.levis.talosmobile.ui.theme.LocalStatusColors
import java.text.DateFormat
import java.util.Date

@get:StringRes
val UpgradePhase.label: Int
    get() = when (this) {
        UpgradePhase.REQUESTED -> R.string.upgrade_phase_requested
        UpgradePhase.INSTALLING -> R.string.upgrade_phase_installing
        UpgradePhase.REBOOTING -> R.string.upgrade_phase_rebooting
        UpgradePhase.WAITING -> R.string.upgrade_phase_waiting
        UpgradePhase.BOOTED -> R.string.upgrade_phase_booted
        UpgradePhase.DONE -> R.string.upgrade_phase_done
    }

/** The followed upgrade: phase timeline, then the result. It cannot be cancelled, only unfollowed. */
@Composable
fun UpgradeProgress(run: UpgradeRunState, onStopFollowing: () -> Unit, onClose: () -> Unit) {
    val colors = LocalStatusColors.current
    var confirmStop by remember { mutableStateOf(false) }
    val steps = remember(run.events, run.finished, run.error) { upgradeTimeline(run.events, run.finished, run.error != null) }
    if (run.running) KeepScreenOn()

    Column(
        Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text(run.image, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace)
        Card(Modifier.fillMaxWidth()) {
            Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                steps.forEach { TimelineRow(it) }
            }
        }
        when {
            run.running -> {
                Text(stringResource(R.string.upgrade_cannot_cancel), style = MaterialTheme.typography.bodySmall)
                OutlinedButton(onClick = { confirmStop = true }, modifier = Modifier.fillMaxWidth()) {
                    Text(stringResource(R.string.upgrade_stop_following))
                }
            }
            run.error != null -> {
                Text(stringResource(R.string.upgrade_failed, run.error), color = colors.bad)
                Button(onClick = onClose, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.common_ok)) }
            }
            else -> {
                Text(
                    stringResource(R.string.upgrade_done_versions, run.fromVersion.ifEmpty { "?" }, run.newVersion.ifEmpty { "?" }),
                    color = colors.ok,
                    style = MaterialTheme.typography.titleMedium,
                )
                Button(onClick = onClose, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.common_ok)) }
            }
        }
    }

    if (confirmStop) {
        AlertDialog(
            onDismissRequest = { confirmStop = false },
            title = { Text(stringResource(R.string.upgrade_stop_following_title)) },
            text = { Text(stringResource(R.string.upgrade_stop_following_body, run.hostname)) },
            confirmButton = {
                TextButton(onClick = {
                    confirmStop = false
                    onStopFollowing()
                }) { Text(stringResource(R.string.upgrade_stop_following)) }
            },
            dismissButton = { TextButton(onClick = { confirmStop = false }) { Text(stringResource(R.string.common_cancel)) } },
        )
    }
}

@Composable
private fun TimelineRow(step: TimelineStep) {
    val colors = LocalStatusColors.current
    val time = remember(step.at) { if (step.at > 0) DateFormat.getTimeInstance(DateFormat.MEDIUM).format(Date(step.at)) else "" }
    Row(verticalAlignment = Alignment.Top) {
        Box(Modifier.size(24.dp), contentAlignment = Alignment.Center) {
            when (step.status) {
                StepStatus.DONE -> Icon(Icons.Outlined.CheckCircle, contentDescription = null, tint = colors.ok)
                StepStatus.CURRENT -> CircularProgressIndicator(Modifier.size(18.dp), strokeWidth = 2.dp)
                StepStatus.FAILED -> Icon(Icons.Outlined.Error, contentDescription = null, tint = colors.bad)
                StepStatus.PENDING -> Icon(Icons.Outlined.RadioButtonUnchecked, contentDescription = null, tint = colors.muted)
            }
        }
        Column(Modifier.weight(1f).padding(start = 12.dp)) {
            Row {
                Text(
                    stringResource(step.phase.label),
                    style = MaterialTheme.typography.bodyLarge,
                    color = if (step.status == StepStatus.PENDING) colors.muted else MaterialTheme.colorScheme.onSurface,
                    modifier = Modifier.weight(1f),
                )
                if (time.isNotEmpty()) {
                    Text(time, style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
            }
            if (step.message.isNotEmpty()) {
                Text(step.message, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
    }
}
