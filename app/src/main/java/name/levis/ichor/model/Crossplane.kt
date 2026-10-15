package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/kube_crossplane.go.

@Serializable
data class Crossplane(
    val installed: Boolean = false,
    val version: String = "",
    val error: String = "",
    /** Problems first. */
    val packages: List<CrossplanePackage> = emptyList(),
    val definitions: List<CrossplaneDefinition> = emptyList(),
    val composites: List<CrossplaneObject> = emptyList(),
    val claims: List<CrossplaneObject> = emptyList(),
    /** Problems first, healthy rows capped (see [truncated]). */
    val managed: List<CrossplaneObject> = emptyList(),
    /** Every managed resource, including the omitted healthy ones. */
    val managedTotal: Int = 0,
    val truncated: Boolean = false,
    val counts: CrossplaneCounts = CrossplaneCounts(),
)

@Serializable
data class CrossplaneCounts(val ok: Int = 0, val warning: Int = 0, val critical: Int = 0)

/** Where an object lives: enough for the object screen (group, version, resource, kind). */
@Serializable
data class CrossplaneRef(
    val group: String = "",
    val version: String = "",
    val resource: String = "",
    val kind: String = "",
    val namespace: String = "",
    val name: String = "",
)

@Serializable
data class CrossplaneCondition(
    val status: String = "",
    val reason: String = "",
    val message: String = "",
    /** Unix ms, 0 when unknown. */
    val lastTransition: Long = 0,
)

@Serializable
data class CrossplanePackage(
    val group: String = "",
    val version: String = "",
    val resource: String = "",
    val kind: String = "",
    val namespace: String = "",
    val name: String = "",
    /** The package image, as written. */
    val `package`: String = "",
    val currentRevision: String = "",
    val currentIdentifier: String = "",
    val installed: CrossplaneCondition = CrossplaneCondition(),
    val healthy: CrossplaneCondition = CrossplaneCondition(),
    val health: String = "",
    val reasons: List<String> = emptyList(),
) {
    val serviceHealth: ServiceHealth get() = ServiceHealth.from(health)
    val label: String get() = if (namespace.isEmpty()) name else "$namespace/$name"
    val reasonList: List<CrossplaneReason> get() = reasons.mapNotNull(CrossplaneReason::from)

    fun ref() = KubeObjectRef(group, version, resource, kind, namespace, name, editable = true)
}

@Serializable
data class CrossplaneDefinitionVersion(val name: String = "", val served: Boolean = false, val referenceable: Boolean = false)

@Serializable
data class CrossplaneDefinition(
    val group: String = "",
    val version: String = "",
    val resource: String = "",
    val kind: String = "",
    val namespace: String = "",
    val name: String = "",
    /** The API the XRD defines. */
    val compositeGroup: String = "",
    val compositeKind: String = "",
    val plural: String = "",
    /** LegacyCluster, Namespaced or Cluster. */
    val scope: String = "",
    val claimKind: String = "",
    val claimPlural: String = "",
    val versions: List<CrossplaneDefinitionVersion> = emptyList(),
    val established: CrossplaneCondition = CrossplaneCondition(),
    val offered: CrossplaneCondition = CrossplaneCondition(),
    val compositions: Int = 0,
    val compositionNames: List<String> = emptyList(),
    val composites: Int = 0,
    val health: String = "",
    val reasons: List<String> = emptyList(),
) {
    val serviceHealth: ServiceHealth get() = ServiceHealth.from(health)
    val label: String get() = if (namespace.isEmpty()) name else "$namespace/$name"
    val reasonList: List<CrossplaneReason> get() = reasons.mapNotNull(CrossplaneReason::from)

    fun ref() = KubeObjectRef(group, version, resource, kind, namespace, name, editable = true)
}

/** A composite resource, a claim or a managed resource: the fields they share. */
@Serializable
data class CrossplaneObject(
    val group: String = "",
    val version: String = "",
    val resource: String = "",
    val kind: String = "",
    val namespace: String = "",
    val name: String = "",
    /** Composites and claims: the composition rendering them, and the composite a claim binds to. */
    val composition: String = "",
    val resources: Int = 0,
    val claim: String = "",
    val composite: String = "",
    /** Managed resources. */
    val providerConfig: String = "",
    val externalName: String = "",
    val paused: Boolean = false,
    val ready: CrossplaneCondition = CrossplaneCondition(),
    val synced: CrossplaneCondition = CrossplaneCondition(),
    /** Unix ms, 0 when unknown. */
    val lastTransition: Long = 0,
    val health: String = "",
    val reasons: List<String> = emptyList(),
) {
    val serviceHealth: ServiceHealth get() = ServiceHealth.from(health)
    val label: String get() = if (namespace.isEmpty()) name else "$namespace/$name"
    val reasonList: List<CrossplaneReason> get() = reasons.mapNotNull(CrossplaneReason::from)

    fun ref() = KubeObjectRef(group, version, resource, kind, namespace, name, editable = true)
}

/** Why a Crossplane row is not ok, as the Go core names it. */
enum class CrossplaneReason(val wire: String) {
    SYNC_FAILED("syncFailed"),
    PROVIDER_UNHEALTHY("providerUnhealthy"),
    NOT_READY("notReady"),
    DELETING("deleting"),
    PAUSED("paused"),
    NOT_ESTABLISHED("notEstablished"),
    ;

    companion object {
        fun from(wire: String): CrossplaneReason? = entries.firstOrNull { it.wire == wire }
    }
}

/** Every row of every list, problems first as the Go core sorted them. */
val Crossplane.rows: List<CrossplaneObject>
    get() = composites + claims + managed

/** The rows whose health is not ok, across kinds. */
val Crossplane.problems: List<CrossplaneObject>
    get() = rows.filter { it.serviceHealth.needsAttention }

/** The rows matching the filter and the query (kind, name, external name). */
fun List<CrossplaneObject>.filtered(filter: CrossplaneFilter, query: String): List<CrossplaneObject> {
    val q = query.trim()
    return filter { o ->
        val match = when (filter) {
            CrossplaneFilter.ALL -> true
            CrossplaneFilter.CRITICAL -> o.serviceHealth == ServiceHealth.CRITICAL
            CrossplaneFilter.WARNING -> o.serviceHealth == ServiceHealth.WARNING
        }
        match && (q.isEmpty() || o.kind.contains(q, ignoreCase = true) || o.name.contains(q, ignoreCase = true) ||
            o.externalName.contains(q, ignoreCase = true))
    }
}

enum class CrossplaneFilter { ALL, CRITICAL, WARNING }
