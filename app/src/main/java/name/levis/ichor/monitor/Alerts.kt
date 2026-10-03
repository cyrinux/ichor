package name.levis.ichor.monitor

import name.levis.ichor.model.NodeHealth
import name.levis.ichor.util.daysUntil

/** What an alert is about; Notifications.kt turns it into translated text. */
enum class AlertKind { NODE_READY, NODE_NOT_READY, NODE_UNREACHABLE, ETCD_ALARM, CERT_EXPIRING, CERT_EXPIRED, DATA_PROBLEM, DATA_OK }

/**
 * [key] identifies the subject, so a newer alert replaces the older notification.
 * [subject]: hostname (node), alarm name (etcd) or volume/cluster (data services);
 * [detail]: node address/reason, etcd member, or "system|severity" (data services);
 * [days]: days until (or since) the certificate expiry.
 */
data class Alert(
    val key: String,
    val kind: AlertKind,
    val problem: Boolean,
    val subject: String = "",
    val detail: String = "",
    val days: Int = 0,
)

data class Evaluation(val alerts: List<Alert>, val next: ClusterSnapshot)

/** Days before expiry when the daily "renew your talosconfig" alert starts. */
const val CERT_WARN_DAYS = 7

/**
 * Compares the previous and current snapshots. Only *changes* alert, so a node that stays
 * down notifies once. The first snapshot (or a context switch) is a silent baseline.
 *
 * A check where no node answered says nothing about the cluster (the phone is off its
 * network): it alerts nothing and keeps the previous snapshot, so neither leaving nor
 * coming back notifies for every node.
 */
fun evaluate(prev: ClusterSnapshot?, cur: ClusterSnapshot, nowMillis: Long): Evaluation {
    val alerts = mutableListOf<Alert>()
    val comparable = prev != null && prev.context == cur.context && !prev.unreachableAsAWhole
    val blind = comparable && cur.unreachableAsAWhole

    if (comparable && !blind) {
        cur.nodes.forEach { (addr, state) ->
            val before = prev.nodes[addr] ?: return@forEach
            if (before.health != state.health) alerts += nodeAlert(addr, state)
        }
        if (cur.etcdChecked && prev.etcdChecked) {
            (cur.etcdAlarms - prev.etcdAlarms.toSet()).forEach { alarm ->
                alerts += Alert(
                    key = "etcd:$alarm",
                    kind = AlertKind.ETCD_ALARM,
                    problem = true,
                    subject = alarm.substringAfter(':'),
                    detail = alarm.substringBefore(':'),
                )
            }
        }
    }

    val today = Math.floorDiv(nowMillis, 86_400_000L)
    val lastWarn = prev?.lastCertWarnDay ?: -1
    var warnedDay = lastWarn
    if (cur.certNotAfter > 0 && lastWarn != today) {
        val days = daysUntil(cur.certNotAfter, nowMillis)
        if (days <= CERT_WARN_DAYS) {
            val kind = if (days < 0) AlertKind.CERT_EXPIRED else AlertKind.CERT_EXPIRING
            alerts += Alert("cert", kind, problem = true, days = kotlin.math.abs(days).toInt())
            warnedDay = today
        }
    }

    val data = if (blind) DataState(prev.dataWatched, prev.dataChecked, prev.dataIssues, prev.dataPending) else evaluateData(prev, cur, comparable, alerts)

    val next = if (blind) prev.copy(certNotAfter = cur.certNotAfter) else cur
    return Evaluation(
        alerts,
        next.copy(
            lastCertWarnDay = warnedDay,
            dataWatched = data.watched,
            dataChecked = data.checked,
            dataIssues = data.issues,
            dataPending = data.pending,
        ),
    )
}

private data class DataState(val watched: Boolean, val checked: Boolean, val issues: Map<String, String>, val pending: List<String>)

/**
 * Longhorn, Garage and CloudNativePG issues, diffed like etcd alarms: a critical issue alerts at
 * once, a warning only when seen on two checks in a row; an issue that clears says so once. The
 * first check (or a context switch, or turning watching on) is a silent baseline; a check that
 * could not read them keeps what was known; turning watching off forgets it.
 */
private fun evaluateData(prev: ClusterSnapshot?, cur: ClusterSnapshot, comparable: Boolean, alerts: MutableList<Alert>): DataState {
    if (!cur.dataWatched) return DataState(watched = false, checked = false, issues = emptyMap(), pending = emptyList())
    val known = prev?.takeIf { comparable && it.dataWatched && it.dataChecked }
    if (!cur.dataChecked) {
        return known?.let { DataState(true, true, it.dataIssues, it.dataPending) } ?: DataState(true, false, emptyMap(), emptyList())
    }
    if (known == null) return DataState(true, true, cur.dataIssues, emptyList())

    val notified = mutableMapOf<String, String>()
    val pending = mutableListOf<String>()
    cur.dataIssues.forEach { (key, severity) ->
        val before = known.dataIssues[key]
        when {
            // Already notified at this severity or a worse one: quiet.
            before == severity || (before == DATA_CRITICAL && severity == DATA_WARNING) -> notified[key] = severity
            // New or worse critical, or a warning seen for the second time in a row.
            severity == DATA_CRITICAL || key in known.dataPending -> {
                alerts += dataAlert(key, severity, problem = true)
                notified[key] = severity
            }
            else -> pending += key
        }
    }
    (known.dataIssues.keys - cur.dataIssues.keys).sorted().forEach { key ->
        alerts += dataAlert(key, known.dataIssues.getValue(key), problem = false)
    }
    return DataState(true, true, notified, pending)
}

private fun dataAlert(key: String, severity: String, problem: Boolean): Alert = Alert(
    key = "data:$key",
    kind = if (problem) AlertKind.DATA_PROBLEM else AlertKind.DATA_OK,
    problem = problem,
    subject = key.substringAfter('|'),
    detail = key.substringBefore('|') + "|" + severity,
)

private fun nodeAlert(addr: String, state: NodeState): Alert {
    val reason = state.reason.ifBlank { addr }
    return when (state.health) {
        NodeHealth.READY -> Alert("node:$addr", AlertKind.NODE_READY, false, state.hostname, addr)
        NodeHealth.NOT_READY -> Alert("node:$addr", AlertKind.NODE_NOT_READY, true, state.hostname, reason)
        NodeHealth.UNREACHABLE -> Alert("node:$addr", AlertKind.NODE_UNREACHABLE, true, state.hostname, reason)
    }
}
