package name.levis.ichor.ui.runbooks

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import name.levis.ichor.ui.design.IchorAssistChip
import androidx.compose.material3.AssistChipDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
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
import name.levis.ichor.model.RunbookStepState
import name.levis.ichor.ui.theme.LocalStatusColors

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

/** The step's action and id, the badge when destructive. */
@Composable
internal fun StepHeadline(action: String, id: String, destructive: Boolean) {
    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        Text("$id · $action", style = MaterialTheme.typography.bodyLarge, modifier = Modifier.weight(1f, fill = false))
        if (destructive) DestructiveBadge()
    }
}
