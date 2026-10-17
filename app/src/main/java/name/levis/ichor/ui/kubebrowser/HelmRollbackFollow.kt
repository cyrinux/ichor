package name.levis.ichor.ui.kubebrowser

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.HelmRollbackFollowProgress
import name.levis.ichor.model.HelmRollbackPhase
import name.levis.ichor.model.HelmRollbackWorkload
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * Following the workloads of a release just rolled back to [revision]. [id] tells this
 * follow's callbacks from a cancelled one's; [result] is the final phase, "" while followed.
 */
data class HelmRollbackFollowState(
    val id: Long,
    val revision: Int,
    val progress: HelmRollbackFollowProgress? = null,
    val result: String = "",
    val error: UiText? = null,
    /** The rollback succeeded but something after it did not (the history was not pruned). */
    val warning: UiText? = null,
) {
    val ended: Boolean get() = result.isNotEmpty() || error != null
}

/** The wait section that replaces the plan once the rollback is done: one row per workload. */
@Composable
internal fun HelmRollbackFollowBody(follow: HelmRollbackFollowState, onStop: () -> Unit) {
    val colors = LocalStatusColors.current
    Text(stringResource(R.string.helm_rollback_done, follow.revision.toString()), style = MaterialTheme.typography.bodyMedium, color = colors.ok)
    follow.warning?.let { InlineError(it.asString()) }
    SectionTitle(stringResource(R.string.helm_rollback_waiting))
    val progress = follow.progress
    if (progress == null) {
        if (!follow.ended) LinearProgressIndicator(Modifier.fillMaxWidth())
    } else {
        LinearProgressIndicator(progress = { progress.fraction }, modifier = Modifier.fillMaxWidth())
        MutedText("${progress.done}/${progress.total}")
        progress.workloads.forEach { WorkloadRow(it) }
    }
    FollowOutcome(follow)
    Row(Modifier.fillMaxWidth().padding(top = 8.dp), horizontalArrangement = Arrangement.End) {
        OutlinedButton(onClick = onStop) {
            Text(stringResource(if (follow.ended) R.string.common_close else R.string.helm_rollback_stop_following))
        }
    }
}

@Composable
private fun FollowOutcome(follow: HelmRollbackFollowState) {
    val colors = LocalStatusColors.current
    val error = follow.error
    if (error != null) {
        InlineError(error.asString())
    } else when (follow.result) {
        HelmRollbackPhase.SUCCEEDED -> Text(stringResource(R.string.helm_rollback_wait_ready), style = MaterialTheme.typography.bodyMedium, color = colors.ok)
        HelmRollbackPhase.FAILED -> {
            val failed = follow.progress?.firstFailed?.label.orEmpty()
            Text(stringResource(R.string.helm_rollback_wait_failed, failed), style = MaterialTheme.typography.bodyMedium, color = colors.bad)
        }
        HelmRollbackPhase.TIMEOUT -> Text(stringResource(R.string.helm_rollback_wait_timeout), style = MaterialTheme.typography.bodyMedium, color = colors.warn)
        else -> Unit
    }
}

@Composable
private fun WorkloadRow(w: HelmRollbackWorkload) {
    val colors = LocalStatusColors.current
    val color = when {
        w.failed -> colors.bad
        w.done -> colors.ok
        else -> colors.warn
    }
    Column(Modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(w.label, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace, modifier = Modifier.weight(1f))
            Text("${w.ready}/${w.desired}", style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace, color = color)
        }
        val fraction = if (w.desired <= 0) (if (w.done) 1f else 0f) else (w.ready.toFloat() / w.desired).coerceIn(0f, 1f)
        LinearProgressIndicator(progress = { fraction }, modifier = Modifier.fillMaxWidth(), color = color)
        if (w.manual) MutedText(stringResource(R.string.helm_rollback_wait_manual))
    }
}
