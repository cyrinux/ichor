package name.levis.ichor.ui.settings

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.model.ALERT_REPEAT_CHOICES
import name.levis.ichor.model.AlertRule
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.QuietHours
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.rememberClusterLabels

/** The quiet-hours choices of a cluster: as the default, none, or a window of its own. */
private enum class QuietChoice { DEFAULT, OFF, CUSTOM }

private val AlertRule.quietChoice: QuietChoice
    get() = when {
        quietHours == null -> QuietChoice.DEFAULT
        quietHours.isOff -> QuietChoice.OFF
        else -> QuietChoice.CUSTOM
    }

/** The stored clusters with a fingerprint, once each (several contexts can be one cluster). */
internal fun ruleClusters(contexts: List<ContextSummary>): List<ContextSummary> =
    contexts.filter { it.fingerprint.isNotBlank() }.distinctBy { it.fingerprint }

/**
 * Per-cluster overrides of the alert rules: for each stored cluster (the demo while it is
 * stored), quiet hours, the minimum severity, the digest and the repeat, each "as default" unless set.
 * A removed cluster's overrides go with it (see [name.levis.ichor.monitor.MonitorStore.retainAlertRules]).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun AlertRulesClusterSheet(app: TalosApp, onDismiss: () -> Unit) {
    val config by app.configRepository.config.collectAsStateWithLifecycle()
    val rules by app.monitorStore.alertRules.collectAsStateWithLifecycle()
    val labels = rememberClusterLabels()
    val clusters = ruleClusters(config?.summary?.contexts.orEmpty())
    var open by rememberSaveable { mutableStateOf<String?>(null) }

    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(
            Modifier.verticalScroll(rememberScrollState()).padding(horizontal = 20.dp).padding(bottom = 16.dp).navigationBarsPadding(),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            Text(stringResource(R.string.alert_rules_cluster_title), style = MaterialTheme.typography.titleLarge)
            if (clusters.isEmpty()) MutedText(stringResource(R.string.alert_rules_no_clusters))
            clusters.forEach { context ->
                val fingerprint = context.fingerprint
                val rule = rules.clusterRule(fingerprint)
                val expanded = open == fingerprint
                HorizontalDivider()
                Column(
                    Modifier.fillMaxWidth().clickable(onClickLabel = labels.of(context)) { open = if (expanded) null else fingerprint }.padding(vertical = 8.dp),
                ) {
                    Text(labels.of(context), style = MaterialTheme.typography.titleSmall, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    MutedText(stringResource(if (rule.isEmpty) R.string.alert_rules_as_default else R.string.alert_rules_own))
                }
                if (expanded) {
                    ClusterRuleEditor(rule) { app.monitorStore.setAlertRules(rules.withCluster(fingerprint, it)) }
                }
            }
        }
    }
}

/** A cluster's four overrides, each with an "as default" choice. */
@Composable
private fun ClusterRuleEditor(rule: AlertRule, onChange: (AlertRule) -> Unit) {
    val asDefault = stringResource(R.string.alert_rules_as_default)
    val off = stringResource(R.string.alert_rules_off)
    val on = stringResource(R.string.alert_rules_on)
    RuleSegments(
        stringResource(R.string.alert_rules_quiet),
        listOf(asDefault, off, stringResource(R.string.alert_rules_custom)),
        rule.quietChoice.ordinal,
        enabled = true,
    ) { i ->
        val quiet = when (QuietChoice.entries[i]) {
            QuietChoice.DEFAULT -> null
            QuietChoice.OFF -> QuietHours.OFF
            QuietChoice.CUSTOM -> rule.quietHours?.takeUnless { it.isOff } ?: QuietHours.NIGHT
        }
        onChange(rule.copy(quietHours = quiet))
    }
    rule.quietHours?.takeUnless { it.isOff }?.let { quiet -> QuietHoursEditor(quiet, enabled = true) { onChange(rule.copy(quietHours = it)) } }
    RuleSegments(
        stringResource(R.string.alert_rules_min_severity),
        listOf(asDefault) + ALERT_RULE_SEVERITIES.map { severityLabel(it) },
        rule.minSeverity?.let { ALERT_RULE_SEVERITIES.indexOf(it) + 1 }?.coerceAtLeast(0) ?: 0,
        enabled = true,
    ) { i -> onChange(rule.copy(minSeverity = if (i == 0) null else ALERT_RULE_SEVERITIES[i - 1])) }
    RuleSegments(
        stringResource(R.string.alert_rules_digest),
        listOf(asDefault, off, on),
        when (rule.digest) {
            null -> 0
            false -> 1
            true -> 2
        },
        enabled = true,
    ) { i -> onChange(rule.copy(digest = if (i == 0) null else i == 2)) }
    RuleSegments(
        stringResource(R.string.alert_repeat_title),
        listOf(asDefault) + ALERT_REPEAT_CHOICES.map { repeatLabel(it) },
        rule.repeatMinutes?.let { ALERT_REPEAT_CHOICES.indexOf(it) + 1 }?.coerceAtLeast(0) ?: 0,
        enabled = true,
    ) { i -> onChange(rule.copy(repeatMinutes = if (i == 0) null else ALERT_REPEAT_CHOICES[i - 1])) }
}
