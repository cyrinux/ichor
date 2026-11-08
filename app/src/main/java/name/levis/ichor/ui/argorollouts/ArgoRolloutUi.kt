package name.levis.ichor.ui.argorollouts

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.StateFlow
import name.levis.ichor.R
import name.levis.ichor.data.ArgoRolloutsRepository
import name.levis.ichor.model.ArgoRollout
import name.levis.ichor.model.ArgoRolloutAction
import name.levis.ichor.ui.KeyedActions
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.components.ResultToasts
import name.levis.ichor.ui.components.Tag
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.uiText
import name.levis.ichor.util.timeAgo

/** Outcome of [action] on the rollout [target] ("namespace/name"): null [error] when it went through. */
data class ArgoRolloutOutcome(val action: ArgoRolloutAction, val target: String, val error: UiText?)

/** Promote, promote fully, abort, retry and pause, run in [scope] (a ViewModel's); [onChanged] after one went through. */
class ArgoRolloutActions(
    private val scope: CoroutineScope,
    private val repository: ArgoRolloutsRepository,
    private val onChanged: () -> Unit,
) {
    private val actions = KeyedActions<ArgoRolloutOutcome>(scope)

    /** Keys ([ArgoRollout.key]) of the rollouts with an action in flight. */
    val busy: StateFlow<Set<String>> get() = actions.busy
    val results: Flow<ArgoRolloutOutcome> get() = actions.results

    fun run(rollout: ArgoRollout, action: ArgoRolloutAction) {
        actions.launch(rollout.key, { repository.act(rollout, action) }, {
            ArgoRolloutOutcome(action, rollout.key, it.exceptionOrNull()?.uiText())
        }, onChanged)
    }
}

/** A toast for each failed action; one that went through shows in the list at the next read. */
@Composable
fun ArgoRolloutResultToasts(results: Flow<ArgoRolloutOutcome>) = ResultToasts(results) { context, r ->
    r.error?.resolve(context)?.let { context.getString(R.string.argo_action_failed, it) to true }
}

/** The action's button label. */
val ArgoRolloutAction.label: Int
    get() = when (this) {
        ArgoRolloutAction.PROMOTE -> R.string.argo_rollout_action_promote
        ArgoRolloutAction.PROMOTE_FULL -> R.string.argo_rollout_action_promote_full
        ArgoRolloutAction.ABORT -> R.string.argo_rollout_action_abort
        ArgoRolloutAction.RETRY -> R.string.argo_rollout_action_retry
        ArgoRolloutAction.PAUSE -> R.string.argo_rollout_action_pause
    }

/** The confirmation's title (takes the rollout's name) and text. */
val ArgoRolloutAction.confirmation: Pair<Int, Int>
    get() = when (this) {
        ArgoRolloutAction.PROMOTE -> R.string.argo_rollout_promote_title to R.string.argo_rollout_promote_text
        ArgoRolloutAction.PROMOTE_FULL -> R.string.argo_rollout_promote_full_title to R.string.argo_rollout_promote_full_text
        ArgoRolloutAction.ABORT -> R.string.argo_rollout_abort_title to R.string.argo_rollout_abort_text
        ArgoRolloutAction.RETRY -> R.string.argo_rollout_retry_title to R.string.argo_rollout_retry_text
        ArgoRolloutAction.PAUSE -> R.string.argo_rollout_pause_title to R.string.argo_rollout_pause_text
    }

/** Canary or Blue-green. */
@Composable
fun StrategyTag(rollout: ArgoRollout, modifier: Modifier = Modifier) {
    val label = when (rollout.strategy) {
        ArgoRollout.CANARY -> stringResource(R.string.argo_rollout_strategy_canary)
        ArgoRollout.BLUE_GREEN -> stringResource(R.string.argo_rollout_strategy_blue_green)
        else -> rollout.strategy.ifEmpty { "-" }
    }
    Tag(label, modifier = modifier)
}

/** "Step 3 of 5", null before the first step or without steps. */
@Composable
fun argoRolloutStepText(rollout: ArgoRollout): String? {
    if (rollout.steps.isEmpty() || rollout.step < 0) return null
    val shown = (rollout.step + 1).coerceAtMost(rollout.steps.size)
    return stringResource(R.string.argo_rollout_step, shown.toString(), rollout.steps.size.toString())
}

/**
 * The canary's steps as segments: done ones green, the current one in the primary colour
 * (red once aborted) and taller, the rest muted; the weight beside. Read out as its step and weight.
 */
@Composable
fun ArgoRolloutStepBar(rollout: ArgoRollout, modifier: Modifier = Modifier) {
    val colors = LocalStatusColors.current
    val done = colors.ok
    val current = if (rollout.aborted) colors.bad else MaterialTheme.colorScheme.primary
    val remaining = MaterialTheme.colorScheme.surfaceVariant
    val weight = stringResource(R.string.argo_rollout_weight, "${rollout.weight}%")
    val description = listOfNotNull(argoRolloutStepText(rollout), weight).joinToString(", ")
    Row(
        modifier.clearAndSetSemantics { contentDescription = description },
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Row(Modifier.weight(1f), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(2.dp)) {
            rollout.steps.indices.forEach { i ->
                val (color, height) = when {
                    i < rollout.step -> done to 6.dp
                    i == rollout.step -> current to 10.dp
                    else -> remaining to 6.dp
                }
                Box(Modifier.weight(1f).height(height).background(color, RoundedCornerShape(3.dp)))
            }
        }
        Text(weight, style = MaterialTheme.typography.labelSmall, maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}

/** "Paused 12 minutes ago · CanaryPauseStep", or "Aborted"; nothing otherwise. */
@Composable
fun ArgoRolloutPauseLine(rollout: ArgoRollout) {
    val colors = LocalStatusColors.current
    when {
        rollout.aborted -> Text(stringResource(R.string.argo_rollout_aborted), style = MaterialTheme.typography.bodySmall, color = colors.bad)
        rollout.paused -> {
            val since = stringResource(R.string.argo_rollout_paused_since, timeAgo(rollout.pausedSince)).trim()
            Text(
                (listOf(since) + rollout.pauseReasons).joinToString("  ·  "),
                style = MaterialTheme.typography.bodySmall,
                color = colors.warn,
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
            )
        }
    }
}
