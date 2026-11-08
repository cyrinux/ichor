package name.levis.ichor.ui.alerts

import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.DoneAll
import androidx.compose.material.icons.outlined.NotificationsOff
import androidx.compose.material.icons.outlined.TimerOff
import androidx.compose.material3.CardDefaults
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.stringResource
import name.levis.ichor.R
import name.levis.ichor.model.AmAlert
import name.levis.ichor.model.AmSilenceState
import name.levis.ichor.model.KubePermission
import name.levis.ichor.model.OnCallIncident
import name.levis.ichor.model.OnCallStatus
import name.levis.ichor.model.OnCallVerb
import name.levis.ichor.model.ShareTarget
import name.levis.ichor.ui.components.QuickActionRow
import name.levis.ichor.ui.components.RowAction
import name.levis.ichor.ui.components.denialReason
import name.levis.ichor.ui.share.rememberShareAction

/** What an alert row's swipe and long press lead to; [silenceDenial]: why the account cannot silence. */
class AlertRowCallbacks(
    val silenceDenial: KubePermission?,
    val onSilence: (AmAlert) -> Unit,
    val onCallBusy: (String) -> Boolean,
    val onAcknowledge: (OnCallIncident) -> Unit,
    /** Asks first: PagerDuty closes the incident for everyone on call. */
    val onResolve: (OnCallIncident) -> Unit,
)

/** What the row offers on an incident in [status]: Acknowledge unless already done, then Resolve. */
internal fun onCallRowVerbs(status: String): List<String> =
    listOfNotNull(OnCallVerb.ACKNOWLEDGE.takeIf { status == OnCallStatus.TRIGGERED }, OnCallVerb.RESOLVE)

/** An expired silence has nothing left to expire. */
internal fun silenceExpirable(state: String): Boolean = state != AmSilenceState.EXPIRED

/**
 * An alert row's quick actions: swipe left for the silence form, prefilled (nothing is sent before
 * its button); a long press lists Silence…, the [incident]'s Acknowledge and Resolve, and Share.
 * A tap still opens the alert ([onClick]).
 */
@Composable
internal fun AlertQuickActions(
    alert: AmAlert,
    incident: OnCallIncident?,
    callbacks: AlertRowCallbacks,
    onClick: () -> Unit,
    content: @Composable () -> Unit,
) {
    val silence = RowAction(
        Icons.Outlined.NotificationsOff,
        stringResource(R.string.row_silence_ellipsis),
        { callbacks.onSilence(alert) },
        enabled = callbacks.silenceDenial == null,
        disabledReason = callbacks.silenceDenial.denialReason(),
    )
    val menu = listOf(silence) + incident?.let { onCallActions(it, callbacks) }.orEmpty() + rememberShareAction(ShareTarget.screen(ShareTarget.ALERTS))
    QuickActionRow(leading = null, trailing = silence, menu = menu, onClick = onClick, content = content)
}

@Composable
private fun onCallActions(incident: OnCallIncident, callbacks: AlertRowCallbacks): List<RowAction> {
    val enabled = !callbacks.onCallBusy(incident.id)
    val acknowledge = RowAction(Icons.Outlined.CheckCircle, stringResource(R.string.oncall_acknowledge), { callbacks.onAcknowledge(incident) }, enabled = enabled)
    val resolve = RowAction(Icons.Outlined.DoneAll, stringResource(R.string.oncall_resolve), { callbacks.onResolve(incident) }, enabled = enabled)
    return onCallRowVerbs(incident.status).map { if (it == OnCallVerb.ACKNOWLEDGE) acknowledge else resolve }
}

/**
 * A silence card's quick actions: swipe left or long-press to Expire, through [onExpire] (its
 * confirmation first). An expired silence has none, and the card stays as it is.
 */
@Composable
internal fun SilenceQuickActions(
    state: String,
    expiring: Boolean,
    denial: KubePermission?,
    onExpire: () -> Unit,
    content: @Composable () -> Unit,
) {
    val expire = RowAction(
        Icons.Outlined.TimerOff,
        stringResource(R.string.alerts_expire),
        onExpire,
        enabled = !expiring && denial == null,
        disabledReason = denial.denialReason(),
        destructive = true,
    ).takeIf { silenceExpirable(state) }
    val shape = CardDefaults.shape
    QuickActionRow(
        leading = null,
        trailing = expire,
        menu = listOfNotNull(expire),
        onClick = null,
        modifier = Modifier.clip(shape),
        shape = shape,
        content = content,
    )
}
