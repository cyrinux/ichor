package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo (see DataServices.kt); the wire format is documented in plans/data-services/README.md.

@Serializable
data class GarageStatus(
    val error: String = "",
    val instances: List<GarageInstance> = emptyList(),
)

@Serializable
data class GarageInstance(
    val namespace: String = "",
    val name: String = "",
    /** Pod the CLI ran in, "" when none was ready. */
    val pod: String = "",
    val pods: Int = 0,
    val podsReady: Int = 0,
    val version: String = "",
    /** healthy, degraded, unavailable or unknown. */
    val status: String = "",
    /** English, causes first (from the Go core). */
    val message: String = "",
    val connectedNodes: Int = 0,
    val knownNodes: Int = 0,
    val storageNodes: Int = 0,
    val storageNodesUp: Int = 0,
    val partitions: Int = 0,
    val partitionsQuorum: Int = 0,
    val partitionsAllOk: Int = 0,
    /** -1 when unknown. */
    val resyncQueue: Long = -1,
    val resyncErrors: Long = -1,
    val tableSyncQueue: Long = -1,
    val layoutVersion: Long = 0,
    val nodes: List<GarageNode> = emptyList(),
    /** cli-json (full picture) or health (status only). */
    val source: String = "",
) {
    val state: GarageState get() = GarageState.from(status)
    val label: String get() = "$namespace/$name"
    val detailed: Boolean get() = source == "cli-json"
}

@Serializable
data class GarageNode(
    val id: String = "",
    val hostname: String = "",
    val zone: String = "",
    val tags: List<String> = emptyList(),
    /** Kubernetes node of the pod with that hostname, "" when none. */
    val kubeNode: String = "",
    /** Holds a role in the current layout: only those count as down. */
    val storage: Boolean = false,
    val up: Boolean = false,
    /** -1 when up or unknown. */
    val lastSeenSecs: Long = -1,
    val draining: Boolean = false,
    val dataAvail: Long = 0,
    val dataTotal: Long = 0,
    val resyncQueue: Long = -1,
    val resyncErrors: Long = -1,
    val tableSyncQueue: Long = -1,
    val statsError: String = "",
    /** Resync tranquility: 0 resyncs at full speed, 2 is Garage's default, -1 when unknown. */
    val tranquility: Long = -1,
) {
    /** What names the node best: Garage forgets a long-gone node's hostname, its tags often name the host. */
    val label: String get() = hostname.ifEmpty { tags.joinToString(",").ifEmpty { id.take(16) } }
}

enum class GarageState(val wire: String, val health: ServiceHealth) {
    HEALTHY("healthy", ServiceHealth.OK),
    DEGRADED("degraded", ServiceHealth.WARNING),
    UNAVAILABLE("unavailable", ServiceHealth.CRITICAL),
    UNKNOWN("unknown", ServiceHealth.UNKNOWN),
    ;

    companion object {
        fun from(wire: String): GarageState = entries.firstOrNull { it.wire == wire } ?: UNKNOWN
    }
}
