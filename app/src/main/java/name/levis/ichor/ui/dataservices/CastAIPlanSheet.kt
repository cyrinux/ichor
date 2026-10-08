package name.levis.ichor.ui.dataservices

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.Card
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.CastAINodeBudget
import name.levis.ichor.model.CastAINodeStatus
import name.levis.ichor.model.CastAIPlan
import name.levis.ichor.model.CastAIPlanEvent
import name.levis.ichor.model.CastAIPlanNode
import name.levis.ichor.model.CastAIPlanState
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.components.localizedDuration
import name.levis.ichor.ui.components.upwardScrollStaysInSheet
import name.levis.ichor.ui.theme.LocalChartColors
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.formatPercent
import name.levis.ichor.util.formatTime
import java.text.DateFormat

/**
 * One consolidation: why it failed, what it costs before and after, each node it removes with
 * the steps it went through (cordoned, blocked by a budget, deleted or given back), the nodes it
 * adds and the NodePool disruption budgets that pace it.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun CastAIPlanSheet(plan: CastAIPlan, onDismiss: () -> Unit) {
    ModalBottomSheet(onDismissRequest = onDismiss, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)) {
        LazyColumn(
            Modifier.upwardScrollStaysInSheet(),
            contentPadding = PaddingValues(start = 16.dp, end = 16.dp, bottom = 24.dp),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            item { Header(plan) }
            item { Pills(plan) }
            if (plan.planState == CastAIPlanState.FAILED || plan.planState == CastAIPlanState.SKIPPED) {
                item { CauseBanner(listOf(failureText(plan), plan.message).filter { it.isNotEmpty() }.distinct().joinToString("\n")) }
            }
            items(plan.warnings) { WarnBanner(it) }
            item { CostCard(plan) }
            if (plan.removing.isNotEmpty()) {
                item { SectionTitle(stringResource(R.string.castai_removing, plan.removing.size)) }
                items(plan.removing, key = { "rm:" + it.name }) { RemovedNode(it) }
            }
            if (plan.adding.isNotEmpty()) {
                item { SectionTitle(stringResource(R.string.castai_adding, plan.adding.size)) }
                items(plan.adding, key = { "add:" + it.name }) { AddedNode(it, plan.currency) }
            }
            if (plan.budgets.isNotEmpty()) {
                item { SectionTitle(stringResource(R.string.castai_budgets)) }
                items(plan.budgets, key = { "b:" + it.nodePool }) { Budget(it) }
            }
        }
    }
}

@Composable
private fun Header(plan: CastAIPlan) {
    Column {
        Text(
            listOfNotNull(stringResource(R.string.castai_consolidation), castAIPlanModeText(plan.planMode)).joinToString(" · "),
            style = MaterialTheme.typography.titleMedium,
        )
        Text(plan.name, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun Pills(plan: CastAIPlan) {
    val muted = LocalStatusColors.current.muted
    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
        StatusPill(castAIPlanStateText(plan.planState), planStateColor(plan.planState))
        if (plan.execute) StatusPill(stringResource(R.string.castai_auto_executed), muted)
        val started = formatTime(plan.createdAt, DateFormat.SHORT)
        val ran = plan.endedAt.takeIf { it > plan.createdAt }?.let { localizedDuration((it - plan.createdAt) / 1000) }
        StatusPill(if (ran != null) stringResource(R.string.castai_started_ran, started, ran) else stringResource(R.string.castai_started, started), muted)
    }
}

/** Monthly cost of the nodes the plan touches, before and after, and what that is for the cluster. */
@Composable
private fun CostCard(plan: CastAIPlan) {
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            Text(stringResource(R.string.castai_cost_title), style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.onSurfaceVariant)
            Row(verticalAlignment = Alignment.Bottom, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(formatMoney(plan.beforeMonthly, plan.currency), style = MaterialTheme.typography.titleMedium, fontFamily = FontFamily.Monospace, color = MaterialTheme.colorScheme.onSurfaceVariant)
                Text("→", color = MaterialTheme.colorScheme.onSurfaceVariant)
                Text(formatMoney(plan.afterMonthly, plan.currency), style = MaterialTheme.typography.headlineSmall, fontFamily = FontFamily.Monospace)
                Box(Modifier.weight(1f))
                Text("−" + formatPercent(plan.savingsPercent, 0), style = MaterialTheme.typography.titleSmall, fontFamily = FontFamily.Monospace, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            BeforeAfterBar(plan.beforeMonthly, plan.afterMonthly, height = 10.dp)
            val share = plan.clusterMonthly.takeIf { it > 0 }?.let { formatPercent(plan.plannedMonthly / it * 100, 1) }
            val lines = listOfNotNull(
                plan.achievedMonthly?.let { stringResource(R.string.castai_cost_measured, formatMoney(it, plan.currency)) },
                share?.let { stringResource(R.string.castai_cost_share, formatMoney(plan.plannedMonthly, plan.currency), it) },
            )
            if (lines.isNotEmpty()) {
                Text(lines.joinToString("\n"), style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
    }
}

/** A node the plan removes: its outcome, then each step with its time. */
@Composable
private fun RemovedNode(node: CastAIPlanNode) {
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(horizontal = 16.dp, vertical = 12.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            NodeTitle(node)
            if (node.events.size > 1 || node.nodeStatus != CastAINodeStatus.SUCCESS) {
                node.events.forEach { EventLine(it) }
            } else {
                node.events.lastOrNull()?.let {
                    Text(stringResource(R.string.castai_node_removed_at, formatTime(it.at, DateFormat.SHORT)), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
            }
        }
    }
}

/** A node the plan adds: its type and price, and how long it took to be ready. */
@Composable
private fun AddedNode(node: CastAIPlanNode, currency: String) {
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(horizontal = 16.dp, vertical = 12.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            NodeTitle(node, node.instanceType.ifEmpty { node.name })
            val details = listOfNotNull(
                stringResource(R.string.castai_spot).takeIf { node.spot },
                node.zone.takeIf { it.isNotEmpty() },
                node.priceHourly.takeIf { it > 0 }?.let { stringResource(R.string.castai_per_hour, formatMoney(it, currency, decimals = 3)) },
            )
            if (details.isNotEmpty()) Text(details.joinToString(" · "), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            val first = node.events.firstOrNull()
            val last = node.events.lastOrNull()
            if (node.nodeStatus == CastAINodeStatus.SUCCESS && first != null && last != null && last.at >= first.at) {
                Text(stringResource(R.string.castai_ready_in, localizedDuration((last.at - first.at) / 1000)), style = MaterialTheme.typography.labelSmall, color = LocalStatusColors.current.ok)
            } else {
                last?.let { EventLine(it) }
            }
        }
    }
}

@Composable
private fun NodeTitle(node: CastAIPlanNode, title: String = node.name) {
    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
        Box(Modifier.size(8.dp).background(nodeStatusColor(node.nodeStatus), CircleShape))
        Text(title, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
        Text(nodeStatusText(node.nodeStatus), style = MaterialTheme.typography.labelSmall, color = nodeStatusColor(node.nodeStatus))
    }
}

/** "18:29  ●  NodePool disruption budget exhausted…", CAST AI's own words for the step. */
@Composable
private fun EventLine(event: CastAIPlanEvent) {
    Row(Modifier.padding(start = 18.dp), verticalAlignment = Alignment.Top, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        Text(formatTime(event.at, DateFormat.SHORT), style = MaterialTheme.typography.labelSmall, fontFamily = FontFamily.Monospace, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.width(52.dp))
        Box(Modifier.padding(top = 4.dp).size(7.dp).background(eventColor(event.status), CircleShape))
        Text(event.description.ifEmpty { event.status }, style = MaterialTheme.typography.labelSmall, color = eventColor(event.status))
    }
}

@Composable
private fun Budget(budget: CastAINodeBudget) {
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(horizontal = 16.dp, vertical = 12.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Row {
                Text(budget.nodePool, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, modifier = Modifier.weight(1f))
                Text(stringResource(R.string.castai_budget_nodes, budget.nodes.toString()), style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            Text(
                stringResource(R.string.castai_budget_detail, budget.allowed.toString(), budget.disrupting.toString()),
                style = MaterialTheme.typography.labelMedium,
                color = if (budget.disrupting >= budget.allowed) LocalStatusColors.current.warn else MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

@Composable
private fun nodeStatusColor(status: CastAINodeStatus): Color = when (status) {
    CastAINodeStatus.SUCCESS -> LocalStatusColors.current.ok
    CastAINodeStatus.FAILED -> LocalStatusColors.current.bad
    CastAINodeStatus.BLOCKED -> LocalStatusColors.current.warn
    CastAINodeStatus.IN_PROGRESS -> LocalChartColors.current.first
    CastAINodeStatus.PENDING -> LocalStatusColors.current.muted
}

@Composable
private fun nodeStatusText(status: CastAINodeStatus): String = stringResource(
    when (status) {
        CastAINodeStatus.SUCCESS -> R.string.castai_node_success
        CastAINodeStatus.FAILED -> R.string.castai_node_failed
        CastAINodeStatus.BLOCKED -> R.string.castai_node_blocked
        CastAINodeStatus.IN_PROGRESS -> R.string.castai_node_in_progress
        CastAINodeStatus.PENDING -> R.string.castai_node_pending
    },
)

/** A step's colour from CAST AI's status: done, failed or given back, waiting, moving. */
@Composable
private fun eventColor(status: String): Color = when (status) {
    "Success" -> LocalStatusColors.current.ok
    "Failed", "NodeUncordoned" -> LocalStatusColors.current.bad
    "Blocked" -> LocalStatusColors.current.warn
    "InProgress" -> LocalChartColors.current.first
    else -> MaterialTheme.colorScheme.onSurfaceVariant
}
