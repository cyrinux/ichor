package name.levis.ichor.model

import kotlinx.serialization.Serializable
import kotlinx.serialization.builtins.MapSerializer
import kotlinx.serialization.builtins.serializer
import kotlinx.serialization.json.Json

// Mirrors go/ichorgo/omni_allocate.go.

/** How a machine set takes its machines. [type]: static, unlimited or none (machines bound by hand). */
@Serializable
data class OmniAllocation(val className: String = "", val type: String = "none", val count: Long = 0)

/** A machine set of the cluster. [matchLabels]: what its machine class asks of a machine. */
@Serializable
data class OmniMachineSet(
    val id: String = "",
    val role: String = "",
    val allocation: OmniAllocation = OmniAllocation(),
    val matchLabels: List<String> = emptyList(),
) {
    val isControlPlane: Boolean get() = role == "controlplane"
}

@Serializable
data class OmniMachineSets(val cluster: String = "", val machineSets: List<OmniMachineSet> = emptyList())

/** A machine that registered in Omni and that no cluster has yet. */
@Serializable
data class OmniMachine(
    val id: String = "",
    val hostname: String = "",
    val addresses: List<String> = emptyList(),
    val talosVersion: String = "",
    val connected: Boolean = false,
    val maintenance: Boolean = false,
    val labels: Map<String, String> = emptyMap(),
) {
    /** What the person reads, and types to confirm. */
    val name: String get() = hostname.ifBlank { id }
}

/** What to allocate where. */
data class OmniAllocateSpec(val machineId: String, val machineSetId: String)

private val labelsSerializer = MapSerializer(String.serializer(), String.serializer())

/**
 * The labels to sign into Omni's join token, as JSON: [role] as ichor/role, then [extra], typed as
 * key=value pairs separated by commas (a bare key has an empty value; the typed one wins).
 */
fun omniLabelsJson(role: String, extra: String): String {
    val typed = extra.split(',').mapNotNull { part ->
        val key = part.substringBefore('=').trim()
        if (key.isEmpty()) null else key to part.substringAfter('=', "").trim()
    }
    return Json.encodeToString(labelsSerializer, mapOf("ichor/role" to role) + typed)
}
