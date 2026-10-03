package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/talosmobile/topology_build.go.

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

/** "FR" -> 🇫🇷 (regional indicator symbols); "" for anything that is not two ASCII letters. */
fun countryFlag(code: String): String {
    if (code.length != 2 || !code.all { it.uppercaseChar() in 'A'..'Z' }) return ""
    return code.uppercase().map { Character.toChars(REGIONAL_INDICATOR_A + (it - 'A')).concatToString() }.joinToString("")
}

private const val REGIONAL_INDICATOR_A = 0x1F1E6
