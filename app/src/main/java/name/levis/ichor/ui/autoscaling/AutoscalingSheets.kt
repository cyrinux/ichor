package name.levis.ichor.ui.autoscaling

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.AUTOSCALE_NOT_READY
import name.levis.ichor.model.AUTOSCALE_OK
import name.levis.ichor.model.AutoscaleTarget
import name.levis.ichor.model.HpaRow
import name.levis.ichor.model.KubeObjectRef
import name.levis.ichor.model.NodeClaimRow
import name.levis.ichor.model.NodePoolRow
import name.levis.ichor.model.ScaledJobRow
import name.levis.ichor.model.ScaledObjectRow
import name.levis.ichor.model.VpaRow
import name.levis.ichor.model.hpaBoundsText
import name.levis.ichor.model.objectRef
import name.levis.ichor.model.vpaChangeText
import name.levis.ichor.ui.components.InfoRow
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.timeAgo

/** One HPA: its metrics against their targets, the three conditions, and what it drives. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun AutoscaleSheet(row: HpaRow, busy: Boolean, onBounds: (Int, Int) -> Unit, onDismiss: () -> Unit, onObject: (KubeObjectRef) -> Unit) {
    Sheet(onDismiss) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(row.name, style = MaterialTheme.typography.titleMedium, fontFamily = FontFamily.Monospace, modifier = Modifier.weight(1f, fill = false))
            AutoscaleStateBadge(row.state)
        }
        MutedText(listOf(row.namespace, targetText(row.target)).filter { it.isNotEmpty() }.joinToString("  ·  "))
        Text(hpaBoundsText(row.current, row.min, row.max), style = MaterialTheme.typography.bodyMedium)
        if (row.desired != row.current) {
            MutedText(stringResource(R.string.autoscaling_desired, row.desired.toString()))
        }
        if (row.lastScaleAt > 0) {
            MutedText(stringResource(R.string.autoscaling_last_scaled, timeAgo(row.lastScaleAt)))
        }
        if (row.managedBy.isNotEmpty()) {
            MutedText(stringResource(R.string.autoscaling_managed_by, row.managedBy, row.scaledObject))
        }

        SectionTitle(stringResource(R.string.autoscaling_metrics))
        if (row.metrics.isEmpty()) {
            MutedText(stringResource(R.string.autoscaling_no_metrics))
        }
        row.metrics.forEach { metric ->
            Row(Modifier.fillMaxWidth().padding(vertical = 2.dp), horizontalArrangement = Arrangement.SpaceBetween) {
                Text(metric.label, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace)
                Text(
                    stringResource(R.string.autoscaling_metric_values, metric.current, metric.target),
                    style = MaterialTheme.typography.bodyMedium,
                    fontFamily = FontFamily.Monospace,
                )
            }
        }

        SectionTitle(stringResource(R.string.autoscaling_conditions))
        ConditionLine(stringResource(R.string.autoscaling_able_to_scale), row.conditions.ableToScale)
        ConditionLine(stringResource(R.string.autoscaling_scaling_active), row.conditions.scalingActive)
        ConditionLine(stringResource(R.string.autoscaling_scaling_limited), row.conditions.scalingLimited)
        if (row.conditions.message.isNotEmpty()) MutedText(row.conditions.message)

        HpaBoundsSection(row, busy, onBounds)

        WorkloadLink(row.namespace, row.target, onObject)
    }
}

/** One KEDA ScaledObject: its triggers, its fallback, and the HPA it owns. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ScaledObjectSheet(row: ScaledObjectRow, busy: Boolean, onPause: (Int) -> Unit, onDismiss: () -> Unit, onObject: (KubeObjectRef) -> Unit) {
    val colors = LocalStatusColors.current
    Sheet(onDismiss) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(row.name, style = MaterialTheme.typography.titleMedium, fontFamily = FontFamily.Monospace, modifier = Modifier.weight(1f, fill = false))
            AutoscaleStateBadge(row.state)
        }
        MutedText(listOf(row.namespace, targetText(row.target)).filter { it.isNotEmpty() }.joinToString("  ·  "))
        Text(hpaBoundsText(row.current, row.min, row.max), style = MaterialTheme.typography.bodyMedium)
        if (row.paused) {
            val paused = row.pausedReplicas?.let { stringResource(R.string.autoscaling_paused_at, it.toString()) } ?: stringResource(R.string.autoscaling_paused)
            Text(paused, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.tertiary)
        }
        if (row.fallback) {
            Text(
                stringResource(R.string.autoscaling_fallback_replicas, row.fallbackReplicas.toString()),
                style = MaterialTheme.typography.bodyMedium,
                color = colors.warn,
            )
        }
        if (row.message.isNotEmpty()) MutedText(row.message)

        SectionTitle(stringResource(R.string.autoscaling_triggers))
        row.triggers.forEach { MutedText(it.label) }
        if (row.triggers.isEmpty()) MutedText(stringResource(R.string.autoscaling_none))

        if (row.hpaName.isNotEmpty()) {
            SectionTitle(stringResource(R.string.autoscaling_owned_hpa))
            MutedText(row.hpaName)
        }

        ScaledObjectPauseSection(row, busy, onPause)

        WorkloadLink(row.namespace, row.target, onObject)
    }
}

/** One KEDA ScaledJob: its triggers and its ceiling. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ScaledJobSheet(row: ScaledJobRow, onDismiss: () -> Unit) {
    Sheet(onDismiss) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(row.name, style = MaterialTheme.typography.titleMedium, fontFamily = FontFamily.Monospace, modifier = Modifier.weight(1f, fill = false))
            AutoscaleStateBadge(row.state)
        }
        MutedText(row.namespace)
        if (row.paused) Text(stringResource(R.string.autoscaling_paused), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.tertiary)
        InfoRow(stringResource(R.string.autoscaling_max_replicas), row.maxReplicaCount.toString())
        if (row.message.isNotEmpty()) MutedText(row.message)

        SectionTitle(stringResource(R.string.autoscaling_triggers))
        row.triggers.forEach { MutedText(it.label) }
        if (row.triggers.isEmpty()) MutedText(stringResource(R.string.autoscaling_none))
    }
}

/** One VPA: what the recommender wants per container, against the live requests. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun VpaSheet(row: VpaRow, onDismiss: () -> Unit, onObject: (KubeObjectRef) -> Unit) {
    Sheet(onDismiss) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(row.name, style = MaterialTheme.typography.titleMedium, fontFamily = FontFamily.Monospace, modifier = Modifier.weight(1f, fill = false))
            AutoscaleStateBadge(row.state)
        }
        MutedText(listOf(row.namespace, targetText(row.target)).filter { it.isNotEmpty() }.joinToString("  ·  "))
        if (row.updateMode.isNotEmpty()) InfoRow(stringResource(R.string.autoscaling_update_mode), row.updateMode)
        if (!row.recommendationProvided) {
            MutedText(stringResource(R.string.autoscaling_no_recommendation))
            if (row.message.isNotEmpty()) MutedText(row.message)
        }

        if (row.containers.isNotEmpty()) {
            SectionTitle(stringResource(R.string.autoscaling_recommendations))
            row.containers.forEach { container ->
                Column(Modifier.fillMaxWidth().padding(vertical = 4.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                    Text(container.name, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace)
                    Text(
                        stringResource(R.string.autoscaling_cpu_change, vpaChangeText(container.cpuRequest, container.cpuTarget)),
                        style = MaterialTheme.typography.bodyMedium,
                    )
                    Text(
                        stringResource(R.string.autoscaling_memory_change, vpaChangeText(container.memoryRequest, container.memoryTarget)),
                        style = MaterialTheme.typography.bodyMedium,
                    )
                    MutedText(
                        stringResource(R.string.autoscaling_bounds, container.cpuLower, container.cpuUpper, container.memoryLower, container.memoryUpper),
                    )
                }
            }
        }

        WorkloadLink(row.namespace, row.target, onObject)
    }
}

/** One Karpenter NodePool: its limits against its usage, its disruption budgets, its claims. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun NodePoolSheet(row: NodePoolRow, claims: List<NodeClaimRow>, onDismiss: () -> Unit, onObject: (KubeObjectRef) -> Unit) {
    val colors = LocalStatusColors.current
    Sheet(onDismiss) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(row.name, style = MaterialTheme.typography.titleMedium, fontFamily = FontFamily.Monospace, modifier = Modifier.weight(1f, fill = false))
            if (row.atLimit) {
                AutoscaleChip(stringResource(R.string.autoscaling_at_limit), colors.bad)
            } else {
                AutoscaleStateBadge(if (row.ready) AUTOSCALE_OK else AUTOSCALE_NOT_READY)
            }
        }
        if (row.message.isNotEmpty()) MutedText(row.message)
        if (row.capacityTypes.isNotEmpty()) {
            MutedText(stringResource(R.string.autoscaling_capacity_types, row.capacityTypes.joinToString(", ")))
        }
        if (row.weight > 0) InfoRow(stringResource(R.string.autoscaling_weight), row.weight.toString())

        SectionTitle(stringResource(R.string.autoscaling_limits))
        Text(
            stringResource(R.string.autoscaling_pool_usage, row.resources.cpu, row.limits.cpu),
            style = MaterialTheme.typography.bodyMedium,
            fontFamily = FontFamily.Monospace,
        )
        Text(
            stringResource(R.string.autoscaling_memory_usage, row.resources.memory, row.limits.memory),
            style = MaterialTheme.typography.bodyMedium,
            fontFamily = FontFamily.Monospace,
        )
        MutedText(stringResource(R.string.autoscaling_pool_nodes, row.resources.nodes.toString()))

        SectionTitle(stringResource(R.string.autoscaling_disruption))
        MutedText(
            listOfNotNull(
                row.disruption.consolidationPolicy,
                row.disruption.consolidateAfter.takeIf { it.isNotEmpty() }?.let { stringResource(R.string.autoscaling_consolidate_after, it) },
            ).joinToString("  ·  "),
        )
        row.disruption.budgets.forEach { MutedText(budgetText(it)) }

        SectionTitle(stringResource(R.string.autoscaling_claims))
        if (claims.isEmpty()) MutedText(stringResource(R.string.autoscaling_none))
        claims.forEach { claim -> ClaimRow(claim, onObject) }
    }
}

@Composable
private fun ClaimRow(claim: NodeClaimRow, onObject: (KubeObjectRef) -> Unit) {
    Column(Modifier.fillMaxWidth().padding(vertical = 4.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(claim.name, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, modifier = Modifier.weight(1f, fill = false))
            NodeClaimBadges(claim)
        }
        MutedText(
            listOfNotNull(
                claim.nodeName.takeIf { it.isNotEmpty() },
                claim.instanceType.takeIf { it.isNotEmpty() },
                claim.capacityType.takeIf { it.isNotEmpty() },
                claim.created.takeIf { it > 0 }?.let { timeAgo(it) },
            ).joinToString("  ·  "),
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
        if (claim.message.isNotEmpty()) Text(claim.message, style = MaterialTheme.typography.bodySmall)
    }
}

/** The workload an autoscaler drives, opening in the Kubernetes browser when the kind is known. */
@Composable
private fun WorkloadLink(namespace: String, target: AutoscaleTarget, onObject: (KubeObjectRef) -> Unit) {
    val ref = target.objectRef(namespace) ?: return
    Column(
        Modifier.fillMaxWidth().clickable(role = Role.Button) { onObject(ref) }.padding(vertical = 8.dp),
    ) {
        Text(
            stringResource(R.string.autoscaling_open_workload, targetText(target)),
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.primary,
        )
    }
}

/** One condition as "name: yes/no", in its colour. */
@Composable
private fun ConditionLine(label: String, ok: Boolean) {
    val colors = LocalStatusColors.current
    Text(
        stringResource(R.string.autoscaling_condition, label, stringResource(if (ok) R.string.autoscaling_yes else R.string.autoscaling_no)),
        style = MaterialTheme.typography.bodyMedium,
        color = if (ok) MaterialTheme.colorScheme.onSurface else colors.bad,
    )
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun Sheet(onDismiss: () -> Unit, content: @Composable ColumnScope.() -> Unit) {
    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(
            Modifier.verticalScroll(rememberScrollState()).padding(horizontal = 16.dp).navigationBarsPadding().padding(bottom = 16.dp),
            verticalArrangement = Arrangement.spacedBy(4.dp),
            content = content,
        )
    }
}
