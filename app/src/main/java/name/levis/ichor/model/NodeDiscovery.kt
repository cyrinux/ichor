package name.levis.ichor.model

import kotlinx.serialization.Serializable

/** The cluster's members as Talos cluster discovery knows them (`talosctl get members`). */
@Serializable
data class NodeDiscovery(
    val context: String,
    val nodes: List<DiscoveredNode> = emptyList(),
) {
    /** Members the talosconfig context does not target yet, with an address to add. */
    val missing: List<DiscoveredNode> get() = nodes.filter { !it.known && it.address.isNotBlank() }
}

/** One cluster member; [known] when the context already targets it (by address or hostname). */
@Serializable
data class DiscoveredNode(
    val address: String,
    val addresses: List<String> = emptyList(),
    val hostname: String = "",
    val role: String = "",
    val known: Boolean = false,
)

/** The [missing] members not set aside with [dismissed] (their addresses). */
fun NodeDiscovery.toOffer(dismissed: Set<String>): List<DiscoveredNode> =
    missing.filter { it.address !in dismissed }
