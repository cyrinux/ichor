package name.levis.ichor.model

import kotlinx.serialization.Serializable

/**
 * What the monitor's KubeSpan alert reads: the broken tunnels of every node's peers. [issues]:
 * "idA|idB" → see [name.levis.ichor.monitor.KubeSpanDetail]. [partial]: a node could not be read,
 * so a tunnel missing from [issues] may only be out of sight.
 */
@Serializable
data class KubeSpanMeshHealth(
    val context: String,
    val checked: Boolean = false,
    val partial: Boolean = false,
    val links: Int = 0,
    val broken: Int = 0,
    val issues: Map<String, String> = emptyMap(),
)
