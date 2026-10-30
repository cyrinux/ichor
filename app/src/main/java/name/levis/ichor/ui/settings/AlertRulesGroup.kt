package name.levis.ichor.ui.settings

import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.model.ALERT_REPEAT_CHOICES
import name.levis.ichor.model.ALERT_SEVERITY_ALL
import name.levis.ichor.model.AlertRule
import name.levis.ichor.model.QuietHours
import name.levis.ichor.ui.components.MutedText

/**
 * The "Rules" group of the background alerts: quiet hours, the minimum severity and the digest
 * by default, and a "Per cluster" button for the overrides (see [AlertRulesClusterSheet]). Read
 * at each check: nothing to confirm, nothing destructive.
 */
@Composable
fun AlertRulesGroup(app: TalosApp, enabled: Boolean) {
    val store = app.monitorStore
    val rules by store.alertRules.collectAsStateWithLifecycle()
    var perCluster by remember { mutableStateOf(false) }
    val rule = rules.default
    val quiet = rule.quietHours?.takeUnless { it.isOff }
    fun update(change: (AlertRule) -> AlertRule) =
        store.setAlertRules(rules.copy(default = change(rules.default)))

    Text(stringResource(R.string.alert_rules_title), style = MaterialTheme.typography.titleMedium)
    MutedText(stringResource(R.string.alert_rules_hint))
    RuleSwitch(stringResource(R.string.alert_rules_quiet), stringResource(R.string.alert_rules_quiet_desc), quiet != null, enabled) { on ->
        update { it.copy(quietHours = if (on) QuietHours.NIGHT else null) }
    }
    if (quiet != null) QuietHoursEditor(quiet, enabled) { q -> update { it.copy(quietHours = q) } }
    val severity = rule.minSeverity ?: ALERT_SEVERITY_ALL
    RuleSegments(
        stringResource(R.string.alert_rules_min_severity),
        ALERT_RULE_SEVERITIES.map { severityLabel(it) },
        ALERT_RULE_SEVERITIES.indexOf(severity).coerceAtLeast(0),
        enabled,
    ) { i -> update { it.copy(minSeverity = ALERT_RULE_SEVERITIES[i].takeUnless { s -> s == ALERT_SEVERITY_ALL }) } }
    RuleSwitch(stringResource(R.string.alert_rules_digest), stringResource(R.string.alert_rules_digest_desc), rule.digest == true, enabled) { on ->
        update { it.copy(digest = on.takeIf { d -> d }) }
    }
    val interval by store.intervalMinutes.collectAsStateWithLifecycle()
    val repeat = rule.repeatMinutes ?: 0
    RuleSegments(
        stringResource(R.string.alert_repeat_title),
        ALERT_REPEAT_CHOICES.map { repeatLabel(it) },
        ALERT_REPEAT_CHOICES.indexOf(repeat).coerceAtLeast(0),
        enabled,
    ) { i -> update { it.copy(repeatMinutes = ALERT_REPEAT_CHOICES[i].takeIf { m -> m > 0 }) } }
    MutedText(stringResource(R.string.alert_repeat_hint, interval.toInt()))
    OutlinedButton(onClick = { perCluster = true }, enabled = enabled, modifier = Modifier.fillMaxWidth()) {
        val count = rules.clusters.size
        Text(
            if (count == 0) {
                stringResource(R.string.alert_rules_per_cluster)
            } else {
                pluralStringResource(R.plurals.alert_rules_per_cluster_count, count, count)
            },
        )
    }
    if (perCluster) AlertRulesClusterSheet(app, onDismiss = { perCluster = false })
}
