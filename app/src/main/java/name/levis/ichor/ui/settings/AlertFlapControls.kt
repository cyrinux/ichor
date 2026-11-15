package name.levis.ichor.ui.settings

import androidx.compose.material3.Slider
import androidx.compose.material3.Text
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import name.levis.ichor.R
import name.levis.ichor.model.ALERT_FLAP_DEFAULT
import name.levis.ichor.model.ALERT_FLAP_RANGE
import name.levis.ichor.model.AlertRule
import kotlin.math.roundToInt

/** The default rule's "Fold flapping alerts into the digest": a switch, then the opens per day it starts from. */
@Composable
fun FlapRule(rule: AlertRule, enabled: Boolean, onChange: (Int?) -> Unit) {
    val opens = rule.flapOpens?.takeIf { it > 0 }
    RuleSwitch(stringResource(R.string.alert_rules_flapping), stringResource(R.string.alert_rules_flapping_desc), opens != null, enabled) { on ->
        onChange(if (on) ALERT_FLAP_DEFAULT else null)
    }
    if (opens != null) FlapSlider(opens, enabled) { onChange(it) }
}

/** A cluster's override: as the default, off, or on from its own opens per day. */
@Composable
fun ClusterFlapRule(rule: AlertRule, onChange: (Int?) -> Unit) {
    val opens = rule.flapOpens
    RuleSegments(
        stringResource(R.string.alert_rules_flapping),
        listOf(stringResource(R.string.alert_rules_as_default), stringResource(R.string.alert_rules_off), stringResource(R.string.alert_rules_on)),
        when {
            opens == null -> 0
            opens <= 0 -> 1
            else -> 2
        },
        enabled = true,
    ) { i -> onChange(listOf(null, 0, opens?.takeIf { it > 0 } ?: ALERT_FLAP_DEFAULT)[i]) }
    if (opens != null && opens > 0) FlapSlider(opens, enabled = true) { onChange(it) }
}

@Composable
private fun FlapSlider(opens: Int, enabled: Boolean, onChange: (Int) -> Unit) {
    val label = stringResource(R.string.alert_rules_flapping_summary, opens)
    Text(label, style = MaterialTheme.typography.bodyMedium)
    Slider(
        value = opens.coerceIn(ALERT_FLAP_RANGE).toFloat(),
        onValueChange = { onChange(it.roundToInt()) },
        valueRange = ALERT_FLAP_RANGE.first.toFloat()..ALERT_FLAP_RANGE.last.toFloat(),
        steps = ALERT_FLAP_RANGE.last - ALERT_FLAP_RANGE.first - 1,
        enabled = enabled,
        modifier = Modifier.semantics { contentDescription = label },
    )
}
