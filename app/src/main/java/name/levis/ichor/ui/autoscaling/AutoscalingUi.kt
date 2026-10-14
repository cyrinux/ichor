package name.levis.ichor.ui.autoscaling

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.AUTOSCALE_AT_MAX
import name.levis.ichor.model.AUTOSCALE_FALLBACK
import name.levis.ichor.model.AUTOSCALE_LIMITED
import name.levis.ichor.model.AUTOSCALE_NOT_READY
import name.levis.ichor.model.AUTOSCALE_OK
import name.levis.ichor.model.AUTOSCALE_PAUSED
import name.levis.ichor.model.AutoscaleTarget
import name.levis.ichor.model.NodeClaimRow
import name.levis.ichor.model.NodePoolBudget
import name.levis.ichor.ui.netpol.TagBadge
import name.levis.ichor.ui.theme.LocalStatusColors

/** The state badge: what went wrong, in its colour (problems first, the Go order). */
@Composable
fun AutoscaleStateBadge(state: String, modifier: Modifier = Modifier) {
    val colors = LocalStatusColors.current
    val (label, color) = when (state) {
        AUTOSCALE_NOT_READY -> stringResource(R.string.autoscaling_not_ready) to colors.bad
        AUTOSCALE_AT_MAX -> stringResource(R.string.autoscaling_at_max) to colors.bad
        AUTOSCALE_LIMITED -> stringResource(R.string.autoscaling_limited) to colors.warn
        AUTOSCALE_FALLBACK -> stringResource(R.string.autoscaling_fallback) to colors.warn
        AUTOSCALE_PAUSED -> stringResource(R.string.autoscaling_paused) to MaterialTheme.colorScheme.tertiary
        AUTOSCALE_OK -> stringResource(R.string.autoscaling_ok) to colors.ok
        else -> state to colors.muted
    }
    val description = stringResource(R.string.autoscaling_state) + ": " + label
    TagBadge(label, color, modifier.semantics { contentDescription = description })
}

/** A small tinted fact, read out as it reads. */
@Composable
fun AutoscaleChip(text: String, color: Color, modifier: Modifier = Modifier) {
    TagBadge(text, color, modifier.semantics { contentDescription = text })
}

/** "Deployment web": the workload an autoscaler drives, as the row's second line shows it. */
fun targetText(target: AutoscaleTarget): String = target.label

/** One disruption budget as one line: its nodes, when, how long, why. */
@Composable
fun budgetText(budget: NodePoolBudget): String = listOfNotNull(
    stringResource(R.string.autoscaling_budget_nodes, budget.nodes),
    budget.schedule.takeIf { it.isNotEmpty() },
    budget.duration.takeIf { it.isNotEmpty() },
    budget.reasons.takeIf { it.isNotEmpty() }?.joinToString(", "),
).joinToString("  ·  ")

/** A NodeClaim's badges: pending, drifted, disrupting — the states worth a look. */
@Composable
fun NodeClaimBadges(claim: NodeClaimRow, modifier: Modifier = Modifier) {
    val colors = LocalStatusColors.current
    Row(modifier, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
        if (claim.pending) AutoscaleChip(stringResource(R.string.autoscaling_pending), colors.bad)
        if (claim.drifted) AutoscaleChip(stringResource(R.string.autoscaling_drifted), colors.warn)
        if (claim.disrupting) AutoscaleChip(stringResource(R.string.autoscaling_disrupting), colors.warn)
        if (!claim.ready && !claim.pending) AutoscaleChip(stringResource(R.string.autoscaling_not_ready), colors.bad)
    }
}
