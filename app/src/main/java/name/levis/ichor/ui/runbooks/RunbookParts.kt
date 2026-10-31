package name.levis.ichor.ui.runbooks

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.size
import name.levis.ichor.ui.design.IchorAssistChip
import androidx.compose.material3.AssistChipDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.activeSummary
import name.levis.ichor.data.pinScope
import name.levis.ichor.model.RunbookPlanStep
import name.levis.ichor.model.RunbookStepState
import name.levis.ichor.ui.plugins.PluginIcon
import name.levis.ichor.ui.theme.LocalStatusColors
import kotlinx.coroutines.delay
import java.util.Locale

/** Where the runbooks of the cluster on screen are kept (as its pins: see [pinScope]); null while none is. */
@Composable
internal fun rememberRunbookScope(): String? {
    val app = LocalContext.current.applicationContext as TalosApp
    val config by app.configRepository.config.collectAsStateWithLifecycle()
    val mask by app.uiPreferences.privacyMask.collectAsStateWithLifecycle()
    return config?.activeSummary?.fingerprint?.takeIf { it.isNotBlank() }?.let { pinScope(it, mask) }
}

/** The "destructive" badge: such a step asks before it runs. */
@Composable
internal fun DestructiveBadge() {
    val label = stringResource(R.string.runbook_destructive)
    val description = stringResource(R.string.runbook_destructive_a11y)
    IchorAssistChip(
        onClick = {},
        label = { Text(label) },
        colors = AssistChipDefaults.assistChipColors(labelColor = LocalStatusColors.current.warn),
        modifier = Modifier.semantics { contentDescription = description },
    )
}

/** A step's state as a word, in its colour. */
@Composable
internal fun StepStateText(state: RunbookStepState) {
    val colors = LocalStatusColors.current
    val (text, color) = when (state.state) {
        RunbookStepState.WAITING -> stringResource(R.string.runbook_waiting) to colors.warn
        RunbookStepState.RUNNING -> stringResource(R.string.runbook_state_running) to MaterialTheme.colorScheme.primary
        RunbookStepState.OK -> stringResource(R.string.runbook_state_ok) to colors.ok
        RunbookStepState.FAILED -> stringResource(R.string.runbook_state_failed) to MaterialTheme.colorScheme.error
        RunbookStepState.SKIPPED -> stringResource(R.string.runbook_state_skipped) to MaterialTheme.colorScheme.onSurfaceVariant
        else -> stringResource(R.string.runbook_state_pending) to MaterialTheme.colorScheme.onSurfaceVariant
    }
    Text(text, color = if (color == Color.Unspecified) MaterialTheme.colorScheme.onSurface else color, style = MaterialTheme.typography.labelMedium)
}

/** The step's id and action (with its plugin's icon and words for a plugin step), the badge when destructive. */
@Composable
internal fun StepHeadline(step: RunbookPlanStep) {
    val action = if (step.isPlugin) {
        stringResource(R.string.runbook_plugin_step, step.pluginName, step.actionLabel, step.target.kind, step.target.name)
    } else {
        step.action
    }
    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        if (step.isPlugin) PluginIcon(step.pluginIcon, step.pluginName, 24.dp)
        Text("${step.id} · $action", style = MaterialTheme.typography.bodyLarge, modifier = Modifier.weight(1f, fill = false))
        if (step.destructive) DestructiveBadge()
    }
}

/** A wait step's ring while it runs: the time waited of its timeout, ticking each second. */
@Composable
internal fun WaitProgress(startedAt: Long, timeoutSeconds: Int) {
    var now by remember { mutableLongStateOf(System.currentTimeMillis()) }
    LaunchedEffect(startedAt) {
        while (true) {
            now = System.currentTimeMillis()
            delay(1_000)
        }
    }
    val waited = waitedSeconds(startedAt, now, timeoutSeconds)
    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        CircularProgressIndicator(progress = { waited.toFloat() / timeoutSeconds }, modifier = Modifier.size(20.dp), strokeWidth = 2.dp)
        Text(
            stringResource(R.string.runbook_wait_elapsed, minutesClock(waited), minutesClock(timeoutSeconds.toLong())),
            style = MaterialTheme.typography.labelMedium,
        )
    }
}

/** The whole seconds waited since [startedAt] (unix ms) at [now], within 0..[timeoutSeconds]. */
internal fun waitedSeconds(startedAt: Long, now: Long, timeoutSeconds: Int): Long =
    ((now - startedAt) / 1_000).coerceIn(0, timeoutSeconds.toLong())

/** Seconds as "m:ss". */
internal fun minutesClock(seconds: Long): String = String.format(Locale.ROOT, "%d:%02d", seconds / 60, seconds % 60)
