package name.levis.ichor.ui.components

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.RadioButtonUnchecked
import androidx.compose.material.icons.outlined.WarningAmber
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.ReadProgress
import name.levis.ichor.model.ReadStep
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * A slow read still loading, step by step (Go ReadProgress): a bar that fills as steps end,
 * "5 of 17 steps", then one row per step, with the count of a fan-out ("12 / 40") and how
 * long a step that took a while lasted. [stepLabel] names a step id.
 */
@Composable
fun ReadProgressBox(progress: ReadProgress, stepLabel: @Composable (String) -> String, modifier: Modifier = Modifier) {
    Column(
        modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(horizontal = 24.dp, vertical = 16.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Text(stringResource(R.string.read_progress_title), style = MaterialTheme.typography.titleMedium)
        val fraction = progress.fraction
        if (fraction != null) {
            LinearProgressIndicator(progress = { fraction }, modifier = Modifier.fillMaxWidth())
            MutedText(stringResource(R.string.read_progress_steps, progress.done.toString(), progress.total.toString()))
        } else {
            LinearProgressIndicator(Modifier.fillMaxWidth())
        }
        Spacer(Modifier.size(4.dp))
        progress.steps.forEach { ReadStepRow(it, stepLabel(it.id)) }
    }
}

@Composable
private fun ReadStepRow(step: ReadStep, label: String) {
    val colors = LocalStatusColors.current
    val detail = when {
        step.state == ReadStep.RUNNING && step.total > 0 ->
            stringResource(R.string.read_progress_count, step.count.toString(), step.total.toString())
        step.state == ReadStep.DONE && step.ms >= 1000 ->
            stringResource(R.string.read_progress_seconds, (step.ms / 1000).toString())
        else -> ""
    }
    val stateText = stringResource(
        when (step.state) {
            ReadStep.DONE -> R.string.read_progress_state_done
            ReadStep.RUNNING -> R.string.read_progress_state_running
            ReadStep.FAILED -> R.string.read_progress_state_failed
            else -> R.string.read_progress_state_pending
        },
    )
    // One spoken line per step: its name, its state, then the count or the duration.
    val spoken = listOf(label, stateText, detail).filter { it.isNotEmpty() }.joinToString(", ")
    Row(
        Modifier.fillMaxWidth().clearAndSetSemantics { contentDescription = spoken },
        verticalAlignment = Alignment.CenterVertically,
    ) {
        when (step.state) {
            ReadStep.DONE -> Icon(Icons.Outlined.CheckCircle, contentDescription = null, tint = colors.ok, modifier = Modifier.size(18.dp))
            ReadStep.FAILED -> Icon(Icons.Outlined.WarningAmber, contentDescription = null, tint = colors.warn, modifier = Modifier.size(18.dp))
            ReadStep.RUNNING -> CircularProgressIndicator(Modifier.size(18.dp), strokeWidth = 2.dp)
            else -> Icon(Icons.Outlined.RadioButtonUnchecked, contentDescription = null, tint = colors.muted, modifier = Modifier.size(18.dp))
        }
        Spacer(Modifier.width(12.dp))
        val pending = step.state == ReadStep.PENDING
        Text(
            label,
            style = MaterialTheme.typography.bodyMedium,
            color = if (pending) MaterialTheme.colorScheme.onSurfaceVariant else MaterialTheme.colorScheme.onSurface,
            modifier = Modifier.weight(1f),
        )
        if (detail.isNotEmpty()) MutedText(detail)
    }
}
