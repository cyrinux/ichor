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

/** The [missing] members not set aside with [dismissed] (any of their addresses, or their hostname). */
fun NodeDiscovery.toOffer(dismissed: Set<String>): List<DiscoveredNode> =
    missing.filter { node -> node.address !in dismissed && node.hostname !in dismissed && node.addresses.none { it in dismissed } }

/** Whether a node can leave the talosconfig context; [endpoint]: the app also connects through it. */
@Serializable
data class ContextNodeRemoval(
    val allowed: Boolean = false,
    val reason: String = "",
    val endpoint: Boolean = false,
)

/** Whether a node can be offered "Forget node": it does not answer and the cluster no longer lists it. */
@Serializable
data class NodeForgetCheck(val forgettable: Boolean = false)
