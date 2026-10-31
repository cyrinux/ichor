package name.levis.ichor.ui.rotateca

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import name.levis.ichor.ui.design.IchorButton
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import name.levis.ichor.ui.design.IchorOutlinedButton
import androidx.compose.material3.Text
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.data.CARotationRunState
import name.levis.ichor.model.CARotationPhase
import name.levis.ichor.model.CARotationPlan
import name.levis.ichor.model.ServiceHealth
import name.levis.ichor.model.caNodeHealth
import name.levis.ichor.model.caRotationTimeline
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.HealthDot
import name.levis.ichor.ui.components.InfoHint
import name.levis.ichor.ui.components.InfoRow
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionCard
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.upgrade.TimelineRow
import name.levis.ichor.util.formatDate

/** The plan: the current CA, the nodes the rotation patches, what stands in its way, and Rotate. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun PlanContent(
    plan: CARotationPlan,
    refreshing: Boolean,
    startError: UiText?,
    modifier: Modifier,
    onRefresh: () -> Unit,
    onStart: () -> Unit,
) {
    val colors = LocalStatusColors.current
    PullToRefreshBox(isRefreshing = refreshing, onRefresh = onRefresh, modifier = modifier.fillMaxSize()) {
        LazyColumn(contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            item { InfoHint(stringResource(R.string.ca_rotate_intro_title), stringResource(R.string.ca_rotate_intro)) }
            plan.ca?.let { ca ->
                item {
                    SectionCard(stringResource(R.string.ca_rotate_current_ca)) {
                        InfoRow(stringResource(R.string.ca_rotate_ca_subject), ca.subject, mono = true)
                        InfoRow(stringResource(R.string.ca_rotate_ca_fingerprint), ca.fingerprint, mono = true)
                        InfoRow(stringResource(R.string.ca_rotate_ca_valid_until), formatDate(ca.notAfter * 1000))
                    }
                }
            }
            item {
                SectionCard(
                    stringResource(R.string.ca_rotate_nodes, plan.nodes.size),
                    subtitle = stringResource(if (plan.discovered) R.string.ca_rotate_nodes_discovered else R.string.ca_rotate_nodes_context),
                    pill = {
                        if (plan.ready) {
                            StatusPill(stringResource(R.string.cp_replace_state_ready), colors.ok)
                        } else {
                            StatusPill(stringResource(R.string.cp_replace_state_blocked), colors.bad)
                        }
                    },
                ) {
                    plan.nodes.forEach { n ->
                        NodeLine(n.hostname.ifBlank { n.node }, "${n.node} · ${n.role} · ${n.version.ifBlank { "?" }}", n.health, n.problem)
                    }
                }
            }
            if (plan.blockers.isNotEmpty()) {
                item {
                    SectionCard(stringResource(R.string.ca_rotate_blockers)) {
                        plan.blockers.forEach { Text(it, color = colors.bad, style = MaterialTheme.typography.bodyMedium) }
                    }
                }
            }
            if (plan.warnings.isNotEmpty()) {
                item {
                    SectionCard(stringResource(R.string.ca_rotate_warnings)) {
                        plan.warnings.forEach { MutedText(it) }
                    }
                }
            }
            startError?.let { item { InlineError(it.asString()) } }
            item {
                IchorButton(onClick = onStart, enabled = plan.ready, modifier = Modifier.fillMaxWidth()) {
                    Text(stringResource(R.string.ca_rotate_start))
                }
            }
        }
    }
}

/** The run: the phases, the nodes of the current one, then how it ended. */
@Composable
internal fun RunContent(
    run: CARotationRunState,
    onScreen: String,
    modifier: Modifier,
    onStop: () -> Unit,
    onIssueConfig: () -> Unit,
    onDone: () -> Unit,
) {
    val colors = LocalStatusColors.current
    val failed = run.error != null
    val steps = caRotationTimeline(run.events, run.finished, failed)
    LazyColumn(modifier.fillMaxSize(), contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        if (run.context != onScreen) {
            item { InfoHint(stringResource(R.string.ca_rotate_title), stringResource(R.string.ca_rotate_other_context, run.context)) }
        }
        item {
            SectionCard(stringResource(R.string.ca_rotate_progress, run.context)) {
                steps.forEach { step -> TimelineRow(stringResource(phaseLabel(step.phase)), step.status, step.at, step.message) }
            }
        }
        run.last?.nodes?.takeIf { it.isNotEmpty() }?.let { nodes ->
            item {
                SectionCard(stringResource(R.string.ca_rotate_nodes, nodes.size)) {
                    nodes.forEach { n -> NodeLine(n.hostname.ifBlank { n.node }, "${n.node} · ${n.role}", caNodeHealth(n.state), "") }
                }
            }
        }
        item {
            when {
                run.running -> Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    MutedText(stringResource(R.string.ca_rotate_running_note))
                    IchorOutlinedButton(onClick = onStop, enabled = !run.stopping, modifier = Modifier.fillMaxWidth()) {
                        Text(stringResource(if (run.stopping) R.string.ca_rotate_stopping else R.string.operation_stop))
                    }
                }
                failed -> SectionCard(stringResource(R.string.ca_rotate_failed_title)) {
                    Text(run.error.orEmpty(), color = colors.bad, style = MaterialTheme.typography.bodyMedium)
                    MutedText(stringResource(if (run.transitionStored) R.string.ca_rotate_failed_after_handover else R.string.ca_rotate_failed_before_handover))
                    IchorButton(onClick = onDone, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.issue_done)) }
                }
                else -> SectionCard(stringResource(R.string.ca_rotate_done_title)) {
                    Text(stringResource(R.string.ca_rotate_done_body), style = MaterialTheme.typography.bodyMedium)
                    run.finalStoreError?.let { MutedText(stringResource(R.string.ca_rotate_final_not_stored, it)) }
                    IchorButton(onClick = onIssueConfig, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.ca_rotate_issue_others)) }
                    IchorOutlinedButton(onClick = onDone, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.issue_done)) }
                }
            }
        }
    }
}

/** A node: its health, name, a muted detail and, when it has one, the problem in red. */
@Composable
private fun NodeLine(name: String, detail: String, health: ServiceHealth, problem: String) {
    Row(Modifier.fillMaxWidth().padding(vertical = 4.dp), verticalAlignment = Alignment.Top) {
        HealthDot(health, Modifier.padding(top = 6.dp, end = 12.dp))
        Column(Modifier.weight(1f)) {
            Text(name, style = MaterialTheme.typography.bodyLarge, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Text(detail, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis)
            if (problem.isNotEmpty()) Text(problem, style = MaterialTheme.typography.bodySmall, color = LocalStatusColors.current.bad)
        }
    }
}

private fun phaseLabel(phase: CARotationPhase): Int = when (phase) {
    CARotationPhase.CHECK -> R.string.ca_rotate_phase_check
    CARotationPhase.ACCEPT -> R.string.ca_rotate_phase_accept
    CARotationPhase.HANDOVER -> R.string.ca_rotate_phase_handover
    CARotationPhase.SWAP -> R.string.ca_rotate_phase_swap
    CARotationPhase.DROP, CARotationPhase.DONE -> R.string.ca_rotate_phase_drop
}
