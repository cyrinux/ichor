package name.levis.ichor.model
import kotlinx.serialization.Serializable

@Serializable
data class LonghornSalvageReplica(val name: String = "", val node: String = "", val healthyAt: Long = 0, val failedAt: Long = 0, val eligible: Boolean = false, val blocked: String = "")
@Serializable
data class LonghornSalvagePreview(val namespace: String = "", val name: String = "", val state: String = "", val robustness: String = "", val canSalvage: Boolean = false, val error: String = "", val replicas: List<LonghornSalvageReplica> = emptyList()) {
    fun accepts(selection: Set<String>): Boolean = canSalvage && error.isEmpty() && selection.isNotEmpty() && selection.all { name -> replicas.any { it.name == name && it.eligible } }
}
