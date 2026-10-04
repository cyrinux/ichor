package name.levis.ichor.model

import kotlinx.serialization.Serializable

/** One Kubernetes node's answer to a public IP probe (a curl pod on it); [error] when none. */
@Serializable
data class PublicIpProbe(
    val name: String = "",
    /** Its InternalIP, to match it to a Talos node. */
    val address: String = "",
    val publicIP: String = "",
    val error: String = "",
)

/** A public IP probe of every node, [at] epoch millis. */
@Serializable
data class PublicIpReport(val nodes: List<PublicIpProbe> = emptyList(), val at: Long = 0)

/** The address [this] found for [node]: by its talosconfig address, else its hostname. */
fun PublicIpReport.ipFor(node: NodeOverview): String? {
    val found = nodes.filter { it.publicIP.isNotEmpty() }
    val byName = node.hostname.takeIf { it.isNotBlank() && it != node.node }?.let { host ->
        found.firstOrNull { it.name.equals(host, ignoreCase = true) }
            ?: found.firstOrNull { shortName(it.name).equals(shortName(host), ignoreCase = true) }
    }
    return (found.firstOrNull { it.address == node.node } ?: byName)?.publicIP
}

private fun shortName(host: String): String = host.trimEnd('.').substringBefore('.')

/** What the Nodes card shows for [this]: what Talos knows, else what a probe found. */
fun NodeOverview.shownPublicIps(probed: PublicIpReport?): List<String> =
    publicIPs.ifEmpty { listOfNotNull(probed?.ipFor(this)) }

/** Whether a probe could find more: a node that answers, with no public IP Talos knows. */
fun ClusterOverview.lacksPublicIps(): Boolean = nodes.any { it.reachable && it.publicIPs.isEmpty() }

/** The first node a probe found nothing for, and why ("" for none). */
fun PublicIpReport.firstError(): String =
    nodes.firstOrNull { it.error.isNotEmpty() }?.let { "${it.name}: ${it.error}" }.orEmpty()
