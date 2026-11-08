package name.levis.ichor.model

import kotlinx.serialization.Serializable

@Serializable
data class CnpgPromotePreview(
    val namespace: String = "", val name: String = "", val currentPrimary: String = "", val instance: String = "",
    val lagBytes: Long? = null, val lagError: String = "", val ready: Boolean = false,
    val canPromote: Boolean = false, val warnings: List<String> = emptyList(),
)

val CnpgCluster.promotablePods: List<CnpgPod>
    get() = if (phase == "Primary instance is being restarted in-place" || hibernated || (targetPrimary.isNotEmpty() && targetPrimary != currentPrimary)) emptyList()
    else instancePods.filter { it.ready && it.name != currentPrimary && it.role != "primary" }
