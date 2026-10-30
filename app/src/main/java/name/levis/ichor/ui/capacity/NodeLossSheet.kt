package name.levis.ichor.ui.capacity

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import name.levis.ichor.ui.design.IchorButton
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import name.levis.ichor.ui.design.IchorTextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.KubeObjectRef
import name.levis.ichor.model.NodeLossPlan
import name.levis.ichor.model.NodeLossSuggestion
import name.levis.ichor.model.objectRef
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * What losing a node — or several picked together — costs: where their pods land, what they
 * evict, what stays Pending, then what to scale down, lowest priority first (system classes
 * never). A suggestion opens the object's scale dialog ([onScale]); the worst case offers the
 * other nodes ([onToggle], several wait for [onSimulate]).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun NodeLossSheet(
    state: NodeLossState,
    onToggle: (String) -> Unit,
    onSimulate: () -> Unit,
    onRetry: () -> Unit,
    onScale: (KubeObjectRef) -> Unit,
    onDismiss: () -> Unit,
) {
    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(
            Modifier.verticalScroll(rememberScrollState()).padding(horizontal = 20.dp).padding(bottom = 16.dp).navigationBarsPadding(),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            when (val s = state.plan) {
                UiState.Loading -> {
                    Text(stringResource(R.string.capacity_loss_loading), style = MaterialTheme.typography.titleMedium)
                    CircularProgressIndicator(Modifier.padding(8.dp))
                }
                is UiState.Failed -> {
                    InlineError(s.message.asString())
                    IchorTextButton(onClick = onRetry) { Text(stringResource(R.string.common_retry)) }
                }
                is UiState.Loaded -> PlanBody(s.data, state, onToggle, onSimulate, onScale)
            }
        }
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun PlanBody(
    plan: NodeLossPlan,
    state: NodeLossState,
    onToggle: (String) -> Unit,
    onSimulate: () -> Unit,
    onScale: (KubeObjectRef) -> Unit,
) {
    val colors = LocalStatusColors.current
    Text(
        stringResource(
            if (plan.nodes.size > 1) R.string.capacity_loss_title_multi else R.string.capacity_loss_title,
            plan.node,
        ),
        style = MaterialTheme.typography.titleMedium,
    )
    // Opened on the worst case: its candidates stay to compare the nodes, several at once.
    if (state.candidates.size > 1) {
        MutedText(stringResource(R.string.capacity_loss_worst, state.worst))
        FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            state.candidates.forEach { c ->
                FilterChip(
                    selected = c.name in state.selected,
                    onClick = { onToggle(c.name) },
                    label = { Text(stringResource(R.string.capacity_loss_candidate, c.name, c.pending.toString())) },
                )
            }
        }
        if (state.selected.size > 1) {
            IchorButton(onClick = onSimulate) { Text(stringResource(R.string.capacity_loss_simulate, state.selected.size)) }
        }
    }
    MutedText(
        stringResource(
            R.string.capacity_loss_lost,
            plan.podsLost.toString(),
            cpuText(plan.cpuLost),
            memoryText(plan.memoryLost),
            plan.daemonSetPods.toString(),
        ),
    )
    Text(stringResource(R.string.capacity_loss_placed, plan.placed.size.toString()), style = MaterialTheme.typography.bodyMedium)

    if (plan.preempted.isNotEmpty()) {
        Text(stringResource(R.string.capacity_loss_preempted_title), style = MaterialTheme.typography.labelLarge)
        plan.preempted.forEach { p ->
            Text(stringResource(R.string.capacity_loss_preempted_row, "${p.namespace}/${p.name}", p.by), style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace)
            if (p.pdbViolated) MutedText(stringResource(R.string.capacity_loss_pdb_violated))
        }
    }

    if (plan.pending.isEmpty()) {
        Text(stringResource(R.string.capacity_loss_all_fit), color = colors.ok, style = MaterialTheme.typography.bodyMedium)
    } else {
        Text(stringResource(R.string.capacity_loss_pending_title), style = MaterialTheme.typography.labelLarge, color = colors.warn)
        plan.pending.forEach { p ->
            Text("${p.namespace}/${p.name}", style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace)
            MutedText(pendingReasonText(p.reason))
        }
    }

    if (plan.suggestions.isNotEmpty()) {
        Text(stringResource(R.string.capacity_loss_suggestions_title), style = MaterialTheme.typography.labelLarge)
        plan.suggestions.forEach { SuggestionRow(it, onScale) }
    }
    if (!plan.covered) Text(stringResource(R.string.capacity_loss_not_enough), color = colors.warn, style = MaterialTheme.typography.bodyMedium)
    if (plan.pending.isNotEmpty()) MutedText(stringResource(R.string.capacity_loss_system_note))

    Text(stringResource(R.string.capacity_loss_estimate), style = MaterialTheme.typography.labelLarge)
    plan.assumptions.forEach { MutedText(it) }
}

/** "Scale Kind ns/name 3 → 2", what it frees, its budget; tapping opens the scale dialog. */
@Composable
private fun SuggestionRow(s: NodeLossSuggestion, onScale: (KubeObjectRef) -> Unit) {
    val colors = LocalStatusColors.current
    val ref = s.objectRef()
    val open = stringResource(R.string.capacity_loss_open_scale)
    Column(
        Modifier.fillMaxWidth()
            .clickable(enabled = ref != null && !s.pdbBlocks, role = Role.Button, onClickLabel = open) { ref?.let(onScale) }
            .padding(vertical = 4.dp),
        verticalArrangement = Arrangement.spacedBy(2.dp),
    ) {
        val subject = "${s.kind} ${s.namespace}/${s.name}"
        Text(
            if (s.pdbBlocks) subject else stringResource(R.string.capacity_loss_suggestion, subject, s.replicas.toString(), s.scaleTo.toString()),
            style = MaterialTheme.typography.bodyMedium,
            fontFamily = FontFamily.Monospace,
        )
        MutedText(
            stringResource(
                R.string.capacity_loss_suggestion_detail,
                if (s.noPriorityClass) stringResource(R.string.capacity_no_class) else s.priorityClassName,
                s.priority.toString(),
                cpuText(s.frees.cpu),
                memoryText(s.frees.memory),
            ),
        )
        when {
            s.pdbBlocks -> Text(stringResource(R.string.capacity_loss_pdb_blocks), color = colors.warn, style = MaterialTheme.typography.bodySmall)
            s.pdb.isNotEmpty() -> MutedText(stringResource(R.string.capacity_loss_pdb_min, s.pdbMinAvailable.toString()))
        }
        if (ref == null) MutedText(stringResource(R.string.capacity_loss_no_scale))
    }
}
