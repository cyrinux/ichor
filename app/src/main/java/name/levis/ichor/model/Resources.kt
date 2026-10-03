package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo resourceTypes, resourceList and resourceGet (`talosctl get`).

@Serializable
data class ResourceType(
    val type: String,
    val aliases: List<String> = emptyList(),
    val namespace: String = "",
    val sensitivity: String = "",
)

/** Sensitive resources (secrets, keys) need os:admin; their detail is never screenshotted. */
val ResourceType.sensitive: Boolean get() = isSensitive(sensitivity)

fun isSensitive(sensitivity: String): Boolean =
    sensitivity.isNotBlank() && !sensitivity.equals("non-sensitive", ignoreCase = true) && !sensitivity.equals("nonsensitive", ignoreCase = true)

/** Resource types of one namespace, for the type list. */
data class ResourceTypeGroup(val namespace: String, val types: List<ResourceType>)

/**
 * Types whose name or one alias contains [query] (case-insensitive; all when blank),
 * grouped by namespace, namespaces and types sorted by name.
 */
fun resourceTypeGroups(types: List<ResourceType>, query: String): List<ResourceTypeGroup> {
    val q = query.trim()
    return types
        .filter { t -> q.isEmpty() || t.type.contains(q, ignoreCase = true) || t.aliases.any { it.contains(q, ignoreCase = true) } }
        .groupBy { it.namespace }
        .toSortedMap()
        .map { (namespace, list) -> ResourceTypeGroup(namespace, list.sortedBy { it.type.lowercase() }) }
}

@Serializable
data class ResourceList(val items: List<ResourceItem> = emptyList(), val truncated: Boolean = false)

@Serializable
data class ResourceItem(
    val id: String,
    val namespace: String = "",
    val version: String = "",
    val phase: String = "",
    /** Epoch milliseconds; 0 when unknown. */
    val updated: Long = 0,
)

/** Items whose id contains [query] (case-insensitive; all when blank). */
fun List<ResourceItem>.matching(query: String): List<ResourceItem> {
    val q = query.trim()
    return if (q.isEmpty()) this else filter { it.id.contains(q, ignoreCase = true) }
}

@Serializable
data class ResourceDetail(val yaml: String = "")
