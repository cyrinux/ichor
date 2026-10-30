package name.levis.ichor.ui.runbooks

import androidx.compose.runtime.setValue
import androidx.compose.runtime.getValue
import name.levis.ichor.ui.components.ClusterScreenTitle
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import name.levis.ichor.ui.design.IchorButton
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import name.levis.ichor.ui.design.IchorOutlinedButton
import name.levis.ichor.ui.design.IchorScaffold
import androidx.compose.material3.Text
import name.levis.ichor.ui.design.IchorTopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.RunbookRunState
import name.levis.ichor.model.RunbookPlanStep
import name.levis.ichor.model.RunbookStepState
import name.levis.ichor.model.confirmsByHostname
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.ConfirmDialog
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.pageContent
import name.levis.ichor.ui.node.HostnameConfirmDialog

/**
 * The runbook running or last run: each step's state, Stop, and before each destructive step
 * its confirmation (the node's hostname typed for a node step); the run waits meanwhile, as
 * long as it takes. Once ended: how each step went and the way to the Activity log.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun RunbookRunScreen(onBack: () -> Unit, onActivity: (cluster: String) -> Unit) {
    val app = LocalContext.current.applicationContext as TalosApp
    val manager = app.runbookManager
    val run by manager.current.collectAsStateWithLifecycle()
    val config by app.configRepository.config.collectAsStateWithLifecycle()
    // The confirmation of the step waiting now opens by itself; closed, a button opens it again.
    var confirming by remember(run?.waiting?.index) { mutableStateOf(true) }

    IchorScaffold(
        topBar = {
            IchorTopAppBar(
                title = { ClusterScreenTitle(run?.plan?.name ?: stringResource(R.string.runbooks_title)) },
                navigationIcon = {
                    BackButton {
                        if (run?.finished == true) manager.clear()
                        onBack()
                    }
                },
            )
        },
    ) { padding ->
        val state = run
        if (state == null) {
            EmptyText(stringResource(R.string.runbook_none_running), Modifier.pageContent(padding))
        } else {
            Column(
                Modifier.pageContent(padding).verticalScroll(rememberScrollState()).padding(16.dp),
                verticalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                RunHeader(state)
                state.plan.steps.forEachIndexed { index, step ->
                    if (index > 0) HorizontalDivider()
                    val st = state.steps.getOrNull(index)
                    Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
                        StepHeadline(step.action, step.id, step.destructive)
                        MutedText(step.target.label)
                        if (step.detail.isNotEmpty()) MutedText(step.detail)
                        st?.let { StepStateText(it) }
                        if (st?.state == RunbookStepState.RUNNING && step.timeoutSeconds > 0) WaitProgress(st.startedAt, step.timeoutSeconds)
                        st?.message?.takeIf { it.isNotEmpty() }?.let { Text(it, fontFamily = FontFamily.Monospace, style = MaterialTheme.typography.bodySmall) }
                    }
                }
                val waiting = state.waiting
                if (waiting != null && !confirming) {
                    IchorButton(onClick = { confirming = true }, modifier = Modifier.fillMaxWidth()) {
                        Text(stringResource(R.string.runbook_confirm))
                    }
                }
                if (state.running) {
                    IchorOutlinedButton(onClick = manager::stop, enabled = !state.stopping, modifier = Modifier.fillMaxWidth()) {
                        Text(stringResource(R.string.operation_stop))
                    }
                } else {
                    IchorButton(onClick = { onActivity(config?.activeContext.orEmpty()) }, modifier = Modifier.fillMaxWidth()) {
                        Text(stringResource(R.string.runbook_activity))
                    }
                }
            }
            state.waiting?.takeIf { confirming }?.let { waiting ->
                state.plan.steps.getOrNull(waiting.index)?.let { ConfirmStep(waiting.index, it, manager::confirm, onDismiss = { confirming = false }) }
            }
        }
    }
}

@Composable
private fun RunHeader(state: RunbookRunState) {
    val step = (state.current + 1).coerceAtMost(state.total)
    Column(Modifier.semantics { liveRegion = LiveRegionMode.Polite }, verticalArrangement = Arrangement.spacedBy(6.dp)) {
        when {
            state.error != null -> Text(state.error, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodyMedium)
            state.finished && state.ok -> Text(stringResource(R.string.runbook_done_ok), style = MaterialTheme.typography.titleMedium)
            state.finished && state.stopping -> Text(stringResource(R.string.runbook_stopped), style = MaterialTheme.typography.titleMedium)
            state.finished -> Text(stringResource(R.string.runbook_done_failed), color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.titleMedium)
            else -> {
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    Text(stringResource(R.string.runbook_step_of, step.toString(), state.total.toString()), style = MaterialTheme.typography.titleMedium)
                    if (state.waiting != null) Text(stringResource(R.string.runbook_waiting), color = MaterialTheme.colorScheme.tertiary)
                }
                LinearProgressIndicator(progress = { if (state.total == 0) 0f else state.current.toFloat() / state.total }, modifier = Modifier.fillMaxWidth())
            }
        }
    }
}

/** The confirmation of the waiting destructive [step]: typed hostname for a node, else a plain dialog. Closed, the run still waits. */
@Composable
private fun ConfirmStep(index: Int, step: RunbookPlanStep, onConfirm: (Int) -> Unit, onDismiss: () -> Unit) {
    val title = stringResource(R.string.runbook_confirm_title, step.id)
    val text = stringResource(R.string.runbook_confirm_text, step.action, step.target.label)
    val confirm = stringResource(R.string.runbook_confirm)
    if (step.confirmsByHostname()) {
        HostnameConfirmDialog(
            title = title,
            hostname = step.target.node,
            confirmLabel = confirm,
            onConfirm = { onConfirm(index) },
            onDismiss = onDismiss,
            emphasized = true,
        ) { Text(text) }
    } else {
        ConfirmDialog(
            title = title,
            text = text,
            confirm = confirm,
            destructive = true,
            onConfirm = { onConfirm(index) },
            onDismiss = onDismiss,
        )
    }
}
