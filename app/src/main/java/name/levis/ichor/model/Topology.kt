package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/topology_build.go.

@Serializable
data class ClusterTopology(
    val nodes: List<TopologyNode> = emptyList(),
    val links: List<TopologyLink> = emptyList(),
    val sites: List<TopologySite> = emptyList(),
)

@Serializable
data class TopologyNode(
    /** Hostname: the key links and sites refer to. */
    val id: String,
    /** The talosconfig target; empty when the node is only known through discovery or its peers. */
    val node: String = "",
    val hostname: String = "",
    val role: String = "",
    val addresses: List<String> = emptyList(),
    val zone: String = "",
    val region: String = "",
    /** ISO 3166-1 alpha-2 code guessed from the zone or region, empty when unknown. */
    val country: String = "",
    val site: String = "",
    val kubespan: Boolean = false,
    val queried: Boolean = false,
    val error: String? = null,
    /** When an unreachable node last answered (epoch millis), kept by the app: see withLastKnown. */
    val lastSeen: Long? = null,
)

@Serializable
data class TopologySite(
    val id: String,
    /** Zone, shared private subnet, or empty. */
    val label: String = "",
    /** "zone", "lan" or "node" (a node alone on its network). */
    val kind: String = "",
    val country: String = "",
    val nodes: List<String> = emptyList(),
)

@Serializable
data class TopologyLink(
    val a: String,
    val b: String,
    /** "up", "down", "degraded" (one end reports it down) or "unknown". */
    val state: String = "unknown",
    val sides: List<TopologyLinkSide> = emptyList(),
)

@Serializable
data class TopologyLinkSide(
    val from: String,
    val to: String,
    val state: String = "unknown",
    val endpoint: String = "",
    /** The endpoint is on a private network: both ends share a LAN. */
    val private: Boolean = false,
    val rx: Long = 0,
    val tx: Long = 0,
    val lastHandshake: Long = 0,
)

val TopologyLink.isBroken: Boolean get() = state == "down" || state == "degraded"

/** Links the node is an end of that are down or degraded. */
fun ClusterTopology.brokenLinks(nodeId: String): Int = links.count { it.isBroken && (it.a == nodeId || it.b == nodeId) }

/** A run of the overview's nodes on one site of the map; [site] null for nodes the map does not know. */
data class NodeGroup(val site: TopologySite?, val nodes: List<NodeOverview>)

/**
 * [nodes] in the map's order: site by site as [ClusterTopology.sites] lists them, control planes
 * first then by hostname within each, and the nodes the map misses last. Without a map (never
 * fetched, or nothing to place), one group with every node in that same order.
 */
fun ClusterTopology?.groupNodes(nodes: List<NodeOverview>): List<NodeGroup> {
    val ordered = nodes.sortedWith(compareBy({ it.role != "controlplane" }, { it.hostname }))
    val sites = this?.sites.orEmpty()
    if (sites.isEmpty()) return listOf(NodeGroup(null, ordered))
    // The map keys nodes by hostname; its talosconfig target is the fallback for a renamed node.
    val byId = sites.flatMap { site -> site.nodes.map { it to site } }.toMap()
    val byTarget = this?.nodes.orEmpty().filter { it.node.isNotBlank() }.mapNotNull { n -> byId[n.id]?.let { n.node to it } }.toMap()
    val siteOf = ordered.associateWith { byId[it.hostname] ?: byTarget[it.node] }
    val placed = sites.mapNotNull { site -> ordered.filter { siteOf[it] == site }.takeIf { it.isNotEmpty() }?.let { NodeGroup(site, it) } }
    val rest = ordered.filter { siteOf[it] == null }
    return if (rest.isEmpty()) placed else placed + NodeGroup(null, rest)
}

/** "FR" -> 🇫🇷 (regional indicator symbols); "" for anything that is not two ASCII letters. */
fun countryFlag(code: String): String {
    if (code.length != 2 || !code.all { it.uppercaseChar() in 'A'..'Z' }) return ""
    return code.uppercase().map { Character.toChars(REGIONAL_INDICATOR_A + (it - 'A')).concatToString() }.joinToString("")
}

private const val REGIONAL_INDICATOR_A = 0x1F1E6
