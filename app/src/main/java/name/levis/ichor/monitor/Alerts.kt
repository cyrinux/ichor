package name.levis.ichor.monitor

import name.levis.ichor.model.NodeHealth
import name.levis.ichor.util.daysUntil

/** What an alert is about; Notifications.kt turns it into translated text. */
enum class AlertKind { NODE_READY, NODE_NOT_READY, NODE_UNREACHABLE, ETCD_ALARM, CERT_EXPIRING, CERT_EXPIRED }

/**
 * [key] identifies the subject, so a newer alert replaces the older notification.
 * [subject]: hostname (node) or alarm name (etcd); [detail]: node address/reason or etcd member;
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

    val next = if (blind) prev.copy(certNotAfter = cur.certNotAfter) else cur
    return Evaluation(alerts, next.copy(lastCertWarnDay = warnedDay))
}

private fun nodeAlert(addr: String, state: NodeState): Alert {
    val reason = state.reason.ifBlank { addr }
    return when (state.health) {
        NodeHealth.READY -> Alert("node:$addr", AlertKind.NODE_READY, false, state.hostname, addr)
        NodeHealth.NOT_READY -> Alert("node:$addr", AlertKind.NODE_NOT_READY, true, state.hostname, reason)
        NodeHealth.UNREACHABLE -> Alert("node:$addr", AlertKind.NODE_UNREACHABLE, true, state.hostname, reason)
    }
}
