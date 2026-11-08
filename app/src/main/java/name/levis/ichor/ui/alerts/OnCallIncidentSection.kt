package name.levis.ichor.ui.alerts

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import name.levis.ichor.ui.design.IchorAlertDialog
import name.levis.ichor.ui.design.IchorButton
import name.levis.ichor.ui.design.IchorOutlinedButton
import androidx.compose.material3.Text
import name.levis.ichor.ui.design.IchorTextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.OnCallConfidence
import name.levis.ichor.model.OnCallIncident
import name.levis.ichor.model.OnCallStatus
import name.levis.ichor.model.OnCallVerb
import name.levis.ichor.model.acknowledgerName
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.theme.LocalStatusColors

/** "Triggered" or "Acknowledged by Sam", read out as being about the on-call incident. */
@Composable
fun OnCallBadge(incident: OnCallIncident, modifier: Modifier = Modifier) {
    val colors = LocalStatusColors.current
    val name = incident.acknowledgerName
    val (label, color) = when {
        incident.status == OnCallStatus.TRIGGERED -> stringResource(R.string.oncall_badge_triggered) to colors.bad
        incident.status != OnCallStatus.ACKNOWLEDGED -> return
        name.isEmpty() -> stringResource(R.string.oncall_badge_acknowledged) to colors.warn
        else -> stringResource(R.string.oncall_badge_acknowledged_by, name) to colors.warn
    }
    val description = stringResource(R.string.oncall_badge_a11y, label)
    StatusPill(label, color, modifier.semantics { contentDescription = description })
}

/**
 * The incident behind an alert in its sheet: its state and title, how sure the match is ([confidence]),
 * Acknowledge unless already done, Resolve after one question, and the link to PagerDuty.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun OnCallIncidentSection(incident: OnCallIncident, confidence: String, busy: Boolean, onAct: (String) -> Unit) {
    val uri = LocalUriHandler.current
    var confirmResolve by remember { mutableStateOf(false) }
    SectionTitle(stringResource(R.string.oncall_section))
    OnCallBadge(incident)
    Text(incident.title)
    MutedText(listOf(stringResource(R.string.oncall_incident_number, incident.number.toString()), incident.service).filter { it.isNotEmpty() }.joinToString(" · "))
    if (confidence != OnCallConfidence.EXACT) MutedText(stringResource(R.string.oncall_match_by_name))
    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        if (incident.status == OnCallStatus.TRIGGERED) {
            IchorButton(onClick = { onAct(OnCallVerb.ACKNOWLEDGE) }, enabled = !busy) { Text(stringResource(R.string.oncall_acknowledge)) }
        }
        IchorOutlinedButton(onClick = { confirmResolve = true }, enabled = !busy) { Text(stringResource(R.string.oncall_resolve)) }
        if (incident.url.isNotEmpty()) {
            IchorOutlinedButton(onClick = { runCatching { uri.openUri(incident.url) } }) { Text(stringResource(R.string.oncall_open)) }
        }
    }
    if (confirmResolve) {
        OnCallResolveDialog(
            incident,
            onConfirm = {
                confirmResolve = false
                onAct(OnCallVerb.RESOLVE)
            },
            onDismiss = { confirmResolve = false },
        )
    }
}

/** The one question before resolving [incident]: PagerDuty closes it for everyone on call. */
@Composable
fun OnCallResolveDialog(incident: OnCallIncident, onConfirm: () -> Unit, onDismiss: () -> Unit) {
    IchorAlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.oncall_resolve_title)) },
        text = { Text(stringResource(R.string.oncall_resolve_body, incident.number.toString())) },
        confirmButton = { IchorTextButton(onClick = onConfirm) { Text(stringResource(R.string.oncall_resolve)) } },
        dismissButton = { IchorTextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}
