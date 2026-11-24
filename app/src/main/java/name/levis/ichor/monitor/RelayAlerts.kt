package name.levis.ichor.monitor

import name.levis.ichor.model.ALERT_ACTION_MUTE
import name.levis.ichor.model.ALERT_REASON_QUIET
import name.levis.ichor.model.ALERT_REASON_SEVERITY
import name.levis.ichor.model.AlertDecisions
import name.levis.ichor.model.AlertInput
import name.levis.ichor.model.RelayAlert
import name.levis.ichor.model.RelayPayload

/**
 * The alerts a push from ichor-relay posts, keyed like the poller's ("am:" + the Alertmanager
 * fingerprint, see amAlert in Alerts.kt) so a pushed alert and its polled twin update one
 * notification: firing ones as AM_FIRING, resolved ones as AM_RESOLVED replacing them. As with
 * the poller, info alerts never notify, and an alert without a fingerprint cannot be keyed.
 */
fun relayAlerts(payload: RelayPayload): List<Alert> = payload.alerts.mapNotNull { alert ->
    if (alert.fingerprint.isBlank() || alert.severity == "info") return@mapNotNull null
    Alert(
        key = "am:${alert.fingerprint}",
        kind = if (alert.firing) AlertKind.AM_FIRING else AlertKind.AM_RESOLVED,
        problem = alert.firing,
        subject = alert.alertname,
        detail = relayDetail(alert),
    )
}

/** The poller's "severity|alertname|where" value (see [AmDetail]): critical, else a warning. */
private fun relayDetail(alert: RelayAlert): String {
    val severity = if (alert.severity == DATA_CRITICAL) DATA_CRITICAL else DATA_WARNING
    return listOf(severity, alert.alertname, alert.namespace).joinToString("|") { it.replace('|', '/') }
}

/** How many alerts the relay left out of [payload] to fit a push message; 0 when none. */
fun relayOmitted(payload: RelayPayload): Int = if (payload.truncated) payload.omitted.coerceAtLeast(0) else 0

/** The pushed [alerts] of the cluster [fingerprint] as Go reads them for the alert rules, in order (see [alertInputs]). */
fun relayInputs(fingerprint: String, alerts: List<Alert>, snoozed: (key: String) -> Boolean): List<AlertInput> = alerts.map { alert ->
    AlertInput(
        cluster = fingerprint,
        key = alert.key,
        kind = alert.ruleKind,
        severity = alert.ruleSeverity,
        problem = alert.problem,
        snoozed = snoozed(alert.key),
    )
}

/** A pushed alert the rules kept quiet: [reason] quiet or severity, [until] the quiet window's end (unix ms). */
data class RelayMuted(val alert: Alert, val reason: String, val until: Long)

/** What a push posts, and what is only recorded for the history. */
data class RelayPlan(val post: List<Alert>, val muted: List<RelayMuted> = emptyList())

/**
 * What to post of the pushed [alerts] given Go's [decisions] over [inputs] (see [relayInputs],
 * same order), like [alertPlan]: null decisions (no rules, or they could not be applied) post
 * every alert not snoozed, and so does an alert Go gave no decision for. One muted by quiet
 * hours or the minimum severity is kept in [RelayPlan.muted]. A digest cluster's alert is
 * posted on its own: a push is not a run, the digest stays the background check's.
 */
fun relayPlan(alerts: List<Alert>, inputs: List<AlertInput>, decisions: AlertDecisions?): RelayPlan {
    val post = mutableListOf<Alert>()
    val muted = mutableListOf<RelayMuted>()
    alerts.forEachIndexed { index, alert ->
        val input = inputs.getOrNull(index)
        val decision = decisions?.decisions?.getOrNull(index)?.takeIf { input != null && it.cluster == input.cluster && it.key == input.key }
        when {
            input?.snoozed == true -> Unit
            decision == null || decision.action != ALERT_ACTION_MUTE -> post.add(alert)
            decision.reason == ALERT_REASON_QUIET || decision.reason == ALERT_REASON_SEVERITY -> muted.add(RelayMuted(alert, decision.reason, decision.until))
        }
    }
    return RelayPlan(post, muted)
}
