package name.levis.ichor.monitor

import name.levis.ichor.model.AmAlerts
import name.levis.ichor.model.AmSeverity
import name.levis.ichor.model.where

/** An Alertmanager alert's "severity|alertname|where" detail, split; a missing part is "". */
data class AmDetail(val severity: String, val alertname: String, val where: String) {
    companion object {
        fun parse(value: String): AmDetail {
            val parts = value.split('|', limit = 3)
            return AmDetail(parts.getOrElse(0) { "" }, parts.getOrElse(1) { "" }, parts.getOrElse(2) { "" })
        }
    }
}

/** The tracked severity of a stored value: critical alerts at once, anything else as a warning. */
fun amSeverity(value: String): String = if (AmDetail.parse(value).severity == DATA_CRITICAL) DATA_CRITICAL else DATA_WARNING

/**
 * The Alertmanager alerts worth a notification, keyed by fingerprint, valued
 * "severity|alertname|where" (see [AmDetail]): those neither silenced nor inhibited, of
 * severity critical, warning or none known. Info alerts (Watchdog, notices) never notify.
 */
fun amIssuesOf(alerts: AmAlerts): Map<String, String> {
    val out = sortedMapOf<String, String>()
    alerts.groups.flatMap { it.alerts }.forEach { alert ->
        if (alert.suppressed || alert.severity == AmSeverity.INFO || alert.fingerprint.isEmpty()) return@forEach
        val severity = if (alert.severity == AmSeverity.CRITICAL) DATA_CRITICAL else DATA_WARNING
        out[alert.fingerprint] = listOf(severity, alert.alertname.clean(), alert.where.clean()).joinToString("|")
    }
    return out
}

/**
 * [amIssuesOf] for a check that may hold only part of the alerts: when Alertmanager had more
 * than the Go core returns ([AmAlerts.truncated]), an issue [known] before but absent here may
 * just be past the cut, so it is kept (never "resolved"); new firing alerts still count.
 */
fun amIssuesWithGaps(alerts: AmAlerts, known: Map<String, String>): Map<String, String> {
    val read = amIssuesOf(alerts)
    if (!alerts.truncated) return read
    return known.filterKeys { it !in read } + read
}

/**
 * The Alertmanager issues a truncated read may carry over: [previous]'s, only when it is the same
 * cluster ([context]) and that check watched and read them. Another cluster's must never leak in.
 */
fun knownAmIssues(previous: ClusterSnapshot?, context: String): Map<String, String> =
    previous?.takeIf { it.context == context && it.amWatched && it.amChecked }?.amIssues.orEmpty()

/** Without the separator, so a value always splits back the same way. */
private fun String.clean(): String = replace('|', '/')
