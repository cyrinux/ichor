package name.levis.ichor.ui.dataservices

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Card
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.CastAIPlan
import name.levis.ichor.model.CastAIPlanMode
import name.levis.ichor.model.CastAIPlanState
import name.levis.ichor.model.CastAIStatus
import name.levis.ichor.model.CastAIStuckNode
import name.levis.ichor.model.planGroups
import name.levis.ichor.model.planSummary
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.theme.LocalChartColors
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.formatTime
import java.text.DateFormat

/**
 * CAST AI's node consolidations (RebalancePlans): nodes it keeps failing to remove first, then
 * what the last day saved and missed, then each plan by state with its cost before and after.
 */
@Composable
fun CastAIPlans(status: CastAIStatus) {
    var selected by rememberSaveable { mutableStateOf<String?>(null) }
    val groups = remember(status) { status.planGroups() }
    val scale = remember(status) { status.plans.maxOfOrNull { it.beforeMonthly } ?: 0.0 }

    LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 16.dp)) {
        if (status.plansError.isNotEmpty()) item { InlineError(stringResource(R.string.data_services_unreadable, status.plansError), Modifier.padding(16.dp)) }
        if (status.plans.isEmpty() && status.plansError.isEmpty()) {
            item { EmptyText(stringResource(R.string.castai_plans_empty)) }
            return@LazyColumn
        }
        items(status.stuck, key = { "stuck:" + it.node }) { StuckBanner(it, Modifier.padding(start = 16.dp, end = 16.dp, top = 10.dp)) }
        item { PlansSummary(status) }
        groups.forEach { (state, plans) ->
            item(key = "h:$state") { GroupHeader(state) }
            items(plans, key = { it.name }) { plan ->
                PlanRow(plan, scale, Modifier.clickable { selected = plan.name })
                HorizontalDivider(color = MaterialTheme.colorScheme.surfaceContainerHigh)
            }
        }
    }

    status.plans.firstOrNull { it.name == selected }?.let { CastAIPlanSheet(it, onDismiss = { selected = null }) }
}

@Composable
private fun StuckBanner(stuck: CastAIStuckNode, modifier: Modifier = Modifier) {
    val text = if (stuck.retrying) {
        stringResource(R.string.castai_stuck_retrying, stuck.node, stuck.failures.toString())
    } else {
        stringResource(R.string.castai_stuck, stuck.node, stuck.failures.toString())
    }
    CauseBanner(text, modifier)
}

/** The last day's plans: what the finished ones saved, what the failed ones did not, by state. */
@Composable
private fun PlansSummary(status: CastAIStatus) {
    val colors = LocalStatusColors.current
    val s = remember(status) { status.planSummary(System.currentTimeMillis()) }
    Card(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 10.dp)) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(14.dp)) {
            Text(stringResource(R.string.castai_plans_last_day), style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.onSurfaceVariant)
            Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                MetricTile(
                    stringResource(R.string.castai_plans_saved), formatMoney(s.savedMonthly, s.currency),
                    stringResource(R.string.castai_per_month_done, s.done.toString()), colors.ok, Modifier.weight(1f),
                )
                MetricTile(
                    stringResource(R.string.castai_plans_missed), formatMoney(s.missedMonthly, s.currency),
                    stringResource(R.string.castai_per_month_failed, s.failed.toString()),
                    if (s.failed > 0) colors.bad else MaterialTheme.colorScheme.onSurfaceVariant, Modifier.weight(1f),
                )
            }
            SplitBar(
                listOf(
                    SplitPart(s.done, colors.ok, stringResource(R.string.castai_plans_count_done, s.done)),
                    SplitPart(s.failed, colors.bad, stringResource(R.string.castai_plans_count_failed, s.failed)),
                    SplitPart(s.running, LocalChartColors.current.first, stringResource(R.string.castai_plans_count_running, s.running)),
                    SplitPart(s.other, colors.muted, stringResource(R.string.castai_plans_count_other, s.other)),
                ),
            )
            if (s.clusterNodes > 0) {
                Text(
                    stringResource(R.string.castai_cluster_cost, formatMoney(s.clusterMonthly, s.currency), s.clusterNodes.toString()),
                    style = MaterialTheme.typography.labelMedium, fontFamily = FontFamily.Monospace, color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }
}

@Composable
private fun GroupHeader(state: CastAIPlanState) {
    Text(
        castAIPlanStateText(state),
        style = MaterialTheme.typography.titleSmall,
        color = planStateColor(state),
        modifier = Modifier.padding(start = 16.dp, end = 16.dp, top = 18.dp, bottom = 4.dp),
    )
}

/** Time, mode, saving; the cost before and after; how many nodes moved; why it failed. */
@Composable
private fun PlanRow(plan: CastAIPlan, scale: Double, modifier: Modifier = Modifier) {
    val state = plan.planState
    Column(modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 10.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            Box(Modifier.size(8.dp).background(planStateColor(state), CircleShape))
            Text(formatTime(plan.createdAt, DateFormat.SHORT), style = MaterialTheme.typography.bodyMedium)
            ModeChip(plan.planMode)
            Box(Modifier.weight(1f))
            Text(savingText(plan), style = MaterialTheme.typography.labelMedium, fontFamily = FontFamily.Monospace, color = savingColor(plan))
        }
        Row(Modifier.padding(start = 18.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            BeforeAfterBar(plan.beforeMonthly, plan.afterMonthly, Modifier.weight(1f), scale = scale)
            Text(
                formatMoney(plan.beforeMonthly, plan.currency) + " → " + formatMoney(plan.afterMonthly, plan.currency),
                style = MaterialTheme.typography.labelSmall, fontFamily = FontFamily.Monospace, color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        Text(nodesText(plan), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(start = 18.dp))
        if (state == CastAIPlanState.FAILED) {
            Text(failureText(plan), style = MaterialTheme.typography.labelSmall, color = LocalStatusColors.current.bad, modifier = Modifier.padding(start = 18.dp))
        }
    }
}

@Composable
fun ModeChip(mode: CastAIPlanMode) {
    val label = castAIPlanModeText(mode) ?: return
    Surface(color = MaterialTheme.colorScheme.surfaceContainerHigh, shape = RoundedCornerShape(6.dp)) {
        Text(label, style = MaterialTheme.typography.labelSmall, modifier = Modifier.padding(horizontal = 8.dp, vertical = 3.dp))
    }
}

/** "−$53/mo" saved, "$141 planned", or "$0 of $141" for a failed plan. */
@Composable
private fun savingText(plan: CastAIPlan): String {
    val planned = formatMoney(plan.plannedMonthly, plan.currency)
    return when (plan.planState) {
        CastAIPlanState.DONE -> {
            val saved = plan.savedMonthly ?: 0.0
            // A plan that ended up costing more is said so, not shown as a negative saving.
            if (saved < 0) stringResource(R.string.castai_cost_up_per_month, formatMoney(-saved, plan.currency))
            else stringResource(R.string.castai_saved_per_month, formatMoney(saved, plan.currency))
        }
        CastAIPlanState.FAILED -> stringResource(R.string.castai_saving_missed, planned)
        else -> stringResource(R.string.castai_saving_planned, planned)
    }
}

@Composable
private fun savingColor(plan: CastAIPlan): Color = when (plan.planState) {
    CastAIPlanState.DONE -> if ((plan.savedMonthly ?: 0.0) < 0) LocalStatusColors.current.bad else LocalStatusColors.current.ok
    CastAIPlanState.FAILED -> LocalStatusColors.current.bad
    else -> MaterialTheme.colorScheme.onSurfaceVariant
}

/** "3 nodes removed of 4 · 1 added". */
@Composable
fun nodesText(plan: CastAIPlan): String = listOfNotNull(
    stringResource(R.string.castai_nodes_removed, plan.removed.toString(), plan.removing.size.toString()).takeIf { plan.removing.isNotEmpty() },
    stringResource(R.string.castai_nodes_added, plan.added.toString(), plan.adding.size.toString()).takeIf { plan.adding.isNotEmpty() },
).joinToString(" · ")

/** "Timeout while removing nodes", in our words when CAST AI's reason and phase are known. */
@Composable
fun failureText(plan: CastAIPlan): String {
    val phase = when (plan.failurePhase) {
        "Deletion" -> stringResource(R.string.castai_phase_deletion)
        "Creation" -> stringResource(R.string.castai_phase_creation)
        else -> plan.failurePhase
    }
    return listOf(plan.failureReason, phase).filter { it.isNotEmpty() }.joinToString(" · ").ifEmpty { plan.message }
}

@Composable
fun planStateColor(state: CastAIPlanState): Color = when (state) {
    CastAIPlanState.DONE -> LocalStatusColors.current.ok
    CastAIPlanState.FAILED -> LocalStatusColors.current.bad
    CastAIPlanState.RUNNING, CastAIPlanState.PENDING -> LocalChartColors.current.first
    CastAIPlanState.AWAITING_APPROVAL -> LocalStatusColors.current.warn
    CastAIPlanState.SKIPPED -> LocalStatusColors.current.muted
}

@Composable
fun castAIPlanStateText(state: CastAIPlanState): String = stringResource(
    when (state) {
        CastAIPlanState.AWAITING_APPROVAL -> R.string.castai_plan_awaiting
        CastAIPlanState.PENDING -> R.string.castai_plan_pending
        CastAIPlanState.RUNNING -> R.string.castai_plan_running
        CastAIPlanState.DONE -> R.string.castai_plan_done
        CastAIPlanState.FAILED -> R.string.castai_plan_failed
        CastAIPlanState.SKIPPED -> R.string.castai_plan_skipped
    },
)

@Composable
fun castAIPlanModeText(mode: CastAIPlanMode): String? = when (mode) {
    CastAIPlanMode.FULL -> stringResource(R.string.castai_mode_full)
    CastAIPlanMode.DELETE_EMPTY -> stringResource(R.string.castai_mode_delete_empty)
    CastAIPlanMode.DRAIN_ONLY -> stringResource(R.string.castai_mode_drain_only)
    CastAIPlanMode.OTHER -> null
}
