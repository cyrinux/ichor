package name.levis.ichor.ui.metrics

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedCard
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.PromHealth
import name.levis.ichor.model.PromLink
import name.levis.ichor.model.PromOperatorServer
import name.levis.ichor.model.PromOperatorStatus
import name.levis.ichor.model.PromRule
import name.levis.ichor.model.PromRuleGroup
import name.levis.ichor.model.PromRules
import name.levis.ichor.model.inTrouble
import name.levis.ichor.model.level
import name.levis.ichor.model.objectRef
import name.levis.ichor.model.ruleRef
import name.levis.ichor.ui.checkup.ageSince
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.expandable
import name.levis.ichor.ui.netpol.TagBadge
import name.levis.ichor.ui.theme.LocalStatusColors

@Composable
internal fun PromHealth.color(): Color {
    val colors = LocalStatusColors.current
    return when (this) {
        PromHealth.CRITICAL -> colors.bad
        PromHealth.WARNING -> colors.warn
        PromHealth.OK -> colors.ok
        PromHealth.UNKNOWN -> colors.muted
    }
}

internal fun LazyListScope.rulesSection(state: MonitoringState, now: Long, onLink: (PromLink) -> Unit) {
    val rules = state.rules.value
    item(key = "rules") { MonitoringHeader(stringResource(R.string.monitoring_rules), rules?.let { rulesSummary(it) }) }
    state.rules.error?.let { item(key = "rules-error") { InlineError(it) } }
    if (rules == null) return
    items(rules.groups, key = { "group-${it.file}-${it.name}" }) { group -> GroupCard(group, now, onLink) }
    if (rules.truncated) item(key = "rules-truncated") { MutedText(stringResource(R.string.monitoring_rules_truncated)) }
}

@Composable
private fun rulesSummary(r: PromRules): String = with(r.counts) {
    stringResource(R.string.monitoring_rule_counts, groups.toString(), rules.toString(), firing.toString(), pending.toString(), errors.toString())
}

/** A rule group: its counts and PrometheusRule, opened on its rules (those in trouble start open). */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun GroupCard(group: PromRuleGroup, now: Long, onLink: (PromLink) -> Unit) {
    val colors = LocalStatusColors.current
    var open by rememberSaveable(group.file, group.name) { mutableStateOf(group.inTrouble) }
    OutlinedCard(Modifier.fillMaxWidth()) {
        Column(Modifier.fillMaxWidth().expandable(open) { open = !open }.padding(12.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text(group.name, style = MaterialTheme.typography.titleSmall, fontFamily = FontFamily.Monospace, maxLines = 2, overflow = TextOverflow.Ellipsis)
            FlowRow(horizontalArrangement = Arrangement.spacedBy(4.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                if (group.errors > 0) TagBadge(stringResource(R.string.monitoring_rule_failing, group.errors.toString()), colors.bad)
                if (group.firing > 0) TagBadge(stringResource(R.string.monitoring_rule_firing, group.firing.toString()), colors.warn)
                if (group.pending > 0) TagBadge(stringResource(R.string.monitoring_rule_pending, group.pending.toString()), colors.muted)
                MutedText(stringResource(R.string.monitoring_rule_total, group.rules.size.toString()))
            }
            if (group.lastEvaluation > 0) MutedText(stringResource(R.string.monitoring_last_evaluation, ageSince(group.lastEvaluation, now)))
        }
        group.ruleRef?.let { ref ->
            TextButton(onClick = { onLink(PromLink.Object(ref)) }, modifier = Modifier.padding(start = 4.dp)) {
                Text(stringResource(R.string.monitoring_rule_from, "${ref.namespace}/${ref.name}"), maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
        }
        if (open) {
            group.rules.forEach { rule ->
                HorizontalDivider()
                RuleRow(rule)
            }
        }
    }
}

/** A rule: its name, type, state and health, and its error when it fails. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun RuleRow(rule: PromRule) {
    val colors = LocalStatusColors.current
    val level = rule.level
    Column(Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(3.dp)) {
        Text(rule.name, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, maxLines = 2, overflow = TextOverflow.Ellipsis)
        FlowRow(horizontalArrangement = Arrangement.spacedBy(4.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            TagBadge(rule.type, MaterialTheme.colorScheme.primary, mono = true)
            if (rule.type == "alerting") TagBadge(rule.state, level.takeIf { rule.state != "inactive" }?.color() ?: colors.muted, mono = true)
            TagBadge(rule.health, if (rule.health == "err") colors.bad else if (rule.health == "ok") colors.ok else colors.muted, mono = true)
            if (rule.severity.isNotEmpty()) TagBadge(rule.severity, colors.muted, mono = true)
            if (rule.alerts > 0) MutedText(stringResource(R.string.monitoring_rule_alerts, rule.alerts.toString()))
        }
        if (rule.lastError.isNotEmpty()) {
            Text(rule.lastError, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace, color = colors.bad, maxLines = 6, overflow = TextOverflow.Ellipsis)
        }
    }
}

/** The operator's servers and counts; nothing when it is not installed (or could not be read). */
internal fun LazyListScope.operatorSection(state: MonitoringState, onLink: (PromLink) -> Unit) {
    val op = state.operator.value
    val error = state.operator.error
    if (op?.installed != true) {
        if (error != null) {
            item(key = "operator") { MonitoringHeader(stringResource(R.string.monitoring_operator), null) }
            item(key = "operator-error") { InlineError(error) }
        }
        return
    }
    item(key = "operator") { MonitoringHeader(stringResource(R.string.monitoring_operator), operatorSummary(op)) }
    listOfNotNull(error, op.error.ifEmpty { null }).forEachIndexed { i, e -> item(key = "operator-error-$i") { InlineError(e) } }
    items(op.prometheuses, key = { "prometheus-${it.namespace}/${it.name}" }) { ServerCard(it, alertmanager = false, onLink) }
    items(op.alertmanagers, key = { "alertmanager-${it.namespace}/${it.name}" }) { ServerCard(it, alertmanager = true, onLink) }
}

@Composable
private fun operatorSummary(op: PromOperatorStatus): String = stringResource(
    R.string.monitoring_operator_counts,
    op.serviceMonitors.toString(),
    op.podMonitors.toString(),
    op.prometheusRules.toString(),
    op.probes.toString(),
)

/** A Prometheus or Alertmanager object: its replicas and conditions; tapping opens it. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun ServerCard(server: PromOperatorServer, alertmanager: Boolean, onLink: (PromLink) -> Unit) {
    val colors = LocalStatusColors.current
    val ref = server.objectRef(alertmanager)
    val kindLabel = if (alertmanager) R.string.monitoring_alertmanager else R.string.monitoring_prometheus
    OutlinedCard(onClick = { onLink(PromLink.Object(ref)) }, modifier = Modifier.fillMaxWidth()) {
        Column(Modifier.padding(12.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Column(Modifier.weight(1f)) {
                    Text("${server.namespace}/${server.name}", style = MaterialTheme.typography.titleSmall, fontFamily = FontFamily.Monospace, maxLines = 2, overflow = TextOverflow.Ellipsis)
                    MutedText(listOf(stringResource(kindLabel), server.version).filter { it.isNotEmpty() }.joinToString(" "), maxLines = 1)
                }
                TagBadge(stringResource(R.string.monitoring_available, server.available.toString(), server.desired.toString()), server.level.color())
            }
            FlowRow(horizontalArrangement = Arrangement.spacedBy(4.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                if (server.paused) TagBadge(stringResource(R.string.monitoring_paused), colors.warn)
                server.conditions.forEach { c -> TagBadge("${c.type}: ${c.status}", c.level.color(), mono = true) }
            }
            server.conditions.filter { it.status != "True" && (it.reason.isNotEmpty() || it.message.isNotEmpty()) }.forEach { c ->
                MutedText(listOf(c.reason, c.message).filter { it.isNotEmpty() }.joinToString(": "), maxLines = 4, overflow = TextOverflow.Ellipsis)
            }
        }
    }
}
