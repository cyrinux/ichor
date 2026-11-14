package name.levis.ichor.monitor

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
