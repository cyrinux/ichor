package name.levis.ichor.monitor

import name.levis.ichor.model.BgpDown
import name.levis.ichor.model.BgpPeerReport

/**
 * A BGP issue's stored value, split: "severity|hostname|instance|peer|state" ([state] the session's
 * FSM state, IDLE or ACTIVE say; [peer] the neighbour's address or "link/<name>").
 */
data class BgpDetail(
    val severity: String,
    val hostname: String,
    val instance: String,
    val peer: String,
    val state: String = "",
) {
    fun format(): String = listOf(severity, hostname.clean(), instance.clean(), peer.clean(), state.clean()).joinToString("|")

    companion object {
        fun parse(value: String): BgpDetail {
            val parts = value.split('|', limit = 5)
            fun part(i: Int) = parts.getOrElse(i) { "" }
            return BgpDetail(part(0), part(1), part(2), part(3), part(4))
        }
    }
}

/**
 * A session down is a warning: it alerts only when still down at the next check, so a node's own
 * reboot (its sessions come back within seconds) or a flap never notifies.
 */
fun bgpSeverity(@Suppress("UNUSED_PARAMETER") value: String): String = DATA_WARNING

/** The issue key of [down]: "node|instance|peer", stable across checks. */
fun bgpIssueKey(down: BgpDown): String = listOf(down.node, down.instance.clean(), down.peer.clean()).joinToString("|")

/**
 * The BGP issues of [report], from the sessions Go judged down. A node that did not answer keeps
 * its [known] issues: unreadable, not resolved. A node without BGP (or older than Talos 1.14) has none.
 */
fun bgpIssuesOf(report: BgpPeerReport, known: Map<String, String> = emptyMap()): Map<String, String> {
    val out = sortedMapOf<String, String>()
    report.nodes.filter { it.error.isNotEmpty() }.forEach { node ->
        out += known.filterKeys { it.substringBefore('|') == node.node }
    }
    report.down.forEach { d ->
        out[bgpIssueKey(d)] = BgpDetail(DATA_WARNING, d.hostname.ifBlank { d.node }, d.instance, d.peer, d.state).format()
    }
    return out
}

/** The BGP issues kept by [previous] when it is the same cluster ([context]) and that check read them. */
fun knownBgpIssues(previous: ClusterSnapshot?, context: String): Map<String, String> =
    previous?.takeIf { it.context == context && it.bgpWatched && it.bgpChecked }?.bgpIssues.orEmpty()

/** The node address of a BGP issue key ("node|instance|peer"). */
fun bgpIssueNode(key: String): String = key.substringBefore('|')

/** Without the separator, so a value always splits back the same way. */
private fun String.clean(): String = replace('|', '/')
