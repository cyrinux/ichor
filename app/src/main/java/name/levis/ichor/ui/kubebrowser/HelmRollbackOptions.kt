package name.levis.ichor.ui.kubebrowser

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.HELM_HISTORY_MAX_LIMIT
import name.levis.ichor.model.HelmRollbackOptions
import name.levis.ichor.model.HelmRollbackPlan
import name.levis.ichor.ui.autoscaling.ReplicaStepper
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.ToggleRow
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * The Options block above the changes: wait for the workloads (only when the target has
 * some), replace instead of patch, and how many revisions to keep. Force and history-max
 * reload the plan ([onChange]); [enabled] false while the rollback runs.
 */
@Composable
internal fun HelmRollbackOptionsBlock(plan: HelmRollbackPlan?, options: HelmRollbackOptions, enabled: Boolean, onChange: (HelmRollbackOptions) -> Unit) {
    SectionTitle(stringResource(R.string.helm_rollback_options))
    Column(Modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        if ((plan?.workloads ?: 0) > 0) {
            ToggleRow(
                title = stringResource(R.string.helm_rollback_wait),
                checked = options.wait,
                onChange = { if (enabled) onChange(options.copy(wait = it)) },
                description = stringResource(R.string.helm_rollback_wait_hint),
            )
        }
        ToggleRow(
            title = stringResource(R.string.helm_rollback_force),
            checked = options.force,
            onChange = { if (enabled) onChange(options.copy(force = it)) },
            description = stringResource(R.string.helm_rollback_force_hint),
        )
        ReplicaStepper(
            label = stringResource(R.string.helm_rollback_history_max),
            value = options.historyMax,
            range = 0..HELM_HISTORY_MAX_LIMIT,
            onChange = { if (enabled) onChange(options.withHistoryMax(it)) },
        )
        MutedText(stringResource(R.string.helm_rollback_history_max_hint))
        val prune = plan?.prune.orEmpty()
        if (prune.isNotEmpty()) {
            Text(
                stringResource(R.string.helm_rollback_prune, prune.joinToString(", ")),
                style = MaterialTheme.typography.bodySmall,
                color = LocalStatusColors.current.warn,
            )
        }
    }
}
