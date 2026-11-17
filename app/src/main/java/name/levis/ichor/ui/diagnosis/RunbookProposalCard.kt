package name.levis.ichor.ui.diagnosis

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Info
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.model.RunbookPlan
import name.levis.ichor.model.RunbookProposal
import name.levis.ichor.model.RunbookRejected
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.design.IchorButton
import name.levis.ichor.ui.design.IchorCard
import name.levis.ichor.ui.design.IchorOutlinedButton
import name.levis.ichor.ui.runbooks.RunbookPlanSteps
import name.levis.ichor.ui.runbooks.rememberRunbookScope
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.userMessage

/**
 * "Turn the fix into a runbook" under a finished answer, then the runbook the model wrote: the
 * planner's plan, the steps the app cannot run, Keep (stored with the cluster's runbooks) and
 * Run (the usual run, which confirms each destructive step and refuses the demo). Nothing is
 * stored until Keep.
 */
@Composable
internal fun RunbookProposalSection(state: ProposalState, onPropose: () -> Unit, onKept: () -> Unit, onRunning: () -> Unit) {
    val proposal = state.proposal
    when {
        state.proposing -> IchorCard(Modifier.fillMaxWidth()) {
            Row(Modifier.padding(16.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp)
                Text(stringResource(R.string.ai_runbook_proposing), style = MaterialTheme.typography.bodySmall)
            }
        }
        proposal != null -> ProposalCard(proposal, state.kept, onKept, onRunning)
        else -> {
            IchorOutlinedButton(onClick = onPropose, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.ai_runbook_propose)) }
            state.error?.let {
                Text(stringResource(R.string.common_stream_failed, it), color = LocalStatusColors.current.bad, style = MaterialTheme.typography.bodyMedium)
            }
        }
    }
}

@Composable
private fun ProposalCard(proposal: RunbookProposal, kept: Boolean, onKept: () -> Unit, onRunning: () -> Unit) {
    val plan = proposal.plan
    IchorCard(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Text(plan?.name ?: stringResource(R.string.ai_runbook_title), style = MaterialTheme.typography.titleMedium)
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Icon(Icons.Outlined.Info, contentDescription = null, modifier = Modifier.size(18.dp))
                MutedText(stringResource(R.string.ai_runbook_hint))
            }
            if (proposal.empty || plan == null) {
                MutedText(stringResource(R.string.ai_runbook_empty))
            } else {
                ProposalPlan(proposal.runbookYAML, plan, kept, onKept, onRunning)
            }
            RejectedSteps(proposal.rejected)
        }
    }
}

@Composable
private fun ProposalPlan(yaml: String, plan: RunbookPlan, kept: Boolean, onKept: () -> Unit, onRunning: () -> Unit) {
    val app = LocalContext.current.applicationContext as TalosApp
    val scope = rememberRunbookScope()
    val coroutines = rememberCoroutineScope()
    var continueIds by remember(plan) { mutableStateOf(emptySet<String>()) }
    var error by remember(plan) { mutableStateOf<String?>(null) }

    if (plan.description.isNotEmpty()) MutedText(plan.description)
    RunbookPlanSteps(plan, continueIds, onToggleContinue = { id, on -> continueIds = if (on) continueIds + id else continueIds - id })
    if (!plan.canRun(continueIds)) {
        Text(stringResource(R.string.runbook_blocked), color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodyMedium)
    }
    error?.let { Text(it, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall) }
    if (kept) {
        MutedText(stringResource(R.string.ai_runbook_kept))
    } else {
        IchorOutlinedButton(
            onClick = {
                val target = scope ?: return@IchorOutlinedButton
                coroutines.launch {
                    try {
                        app.runbookRepository.import(target, yaml)
                        onKept()
                    } catch (e: CancellationException) {
                        throw e
                    } catch (e: Exception) {
                        error = e.userMessage()
                    }
                }
            },
            enabled = scope != null,
            modifier = Modifier.fillMaxWidth(),
        ) { Text(stringResource(R.string.ai_runbook_keep)) }
    }
    IchorButton(
        onClick = {
            // The core refuses the demo before any step: its reason shows on the run screen.
            runCatching { app.runbookManager.start(yaml, plan, continueIds) }
                .onSuccess { started -> if (started) onRunning() else error = app.getString(R.string.runbook_already_running) }
                .onFailure { error = it.userMessage() }
        },
        enabled = plan.canRun(continueIds),
        modifier = Modifier.fillMaxWidth(),
    ) { Text(stringResource(R.string.runbook_run)) }
}

@Composable
private fun RejectedSteps(rejected: List<RunbookRejected>) {
    if (rejected.isEmpty()) return
    Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Text(stringResource(R.string.ai_runbook_rejected), style = MaterialTheme.typography.titleSmall)
        rejected.forEach { step ->
            val line = if (step.id.isEmpty()) step.reason else stringResource(R.string.ai_runbook_rejected_row, step.id, step.reason)
            MutedText(line)
        }
    }
}
