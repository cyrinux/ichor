package dev.talos.viewer.monitor

import dev.talos.viewer.model.NodeHealth
import dev.talos.viewer.util.daysUntil

/** [key] identifies the subject, so a newer alert replaces the older notification. */
data class Alert(val key: String, val title: String, val text: String, val problem: Boolean)

data class Evaluation(val alerts: List<Alert>, val next: ClusterSnapshot)

const val CERT_WARN_DAYS = 14

/**
 * Compares the previous and current snapshots. Only *changes* alert, so a node that stays
 * down notifies once. The first snapshot (or a context switch) is a silent baseline.
 */
fun evaluate(prev: ClusterSnapshot?, cur: ClusterSnapshot, nowMillis: Long): Evaluation {
    val alerts = mutableListOf<Alert>()
    val comparable = prev != null && prev.context == cur.context

    if (comparable) {
        cur.nodes.forEach { (addr, state) ->
            val before = prev!!.nodes[addr] ?: return@forEach
            if (before.health != state.health) alerts += nodeAlert(addr, state)
        }
        if (cur.etcdChecked && prev!!.etcdChecked) {
            (cur.etcdAlarms - prev.etcdAlarms.toSet()).forEach { alarm ->
                alerts += Alert("etcd:$alarm", "etcd alarm raised", alarm.substringAfter(':') + " on member " + alarm.substringBefore(':'), true)
            }
        }
    }

    val today = Math.floorDiv(nowMillis, 86_400_000L)
    val lastWarn = prev?.lastCertWarnDay ?: -1
    var warnedDay = lastWarn
    if (cur.certNotAfter > 0 && lastWarn != today) {
        val days = daysUntil(cur.certNotAfter, nowMillis)
        if (days <= CERT_WARN_DAYS) {
            val text = if (days < 0) "The client certificate expired ${-days} days ago."
            else "The client certificate expires in $days days. Generate a new talosconfig."
            alerts += Alert("cert", "talosconfig certificate", text, true)
            warnedDay = today
        }
    }

    return Evaluation(alerts, cur.copy(lastCertWarnDay = warnedDay))
}

private fun nodeAlert(addr: String, state: NodeState): Alert {
    val reason = state.reason.ifBlank { addr }
    return when (state.health) {
        NodeHealth.READY -> Alert("node:$addr", "${state.hostname} is ready again", addr, false)
        NodeHealth.NOT_READY -> Alert("node:$addr", "${state.hostname} is not ready", reason, true)
        NodeHealth.UNREACHABLE -> Alert("node:$addr", "${state.hostname} is unreachable", reason, true)
    }
}
