package name.levis.ichor.model
import kotlinx.serialization.Serializable
@Serializable
data class LonghornAttachmentNode(val name: String = "", val eligible: Boolean = false)
@Serializable
data class LonghornAttachmentPreview(val namespace: String = "", val name: String = "", val state: String = "", val node: String = "", val canAttach: Boolean = false, val canDetach: Boolean = false, val error: String = "", val nodes: List<LonghornAttachmentNode> = emptyList()) {
    fun accepts(selected: String, attach: Boolean): Boolean = selected.isNotEmpty() && if (attach) error.isEmpty() && canAttach && nodes.any { it.name == selected && it.eligible } else canDetach && selected == node
}
