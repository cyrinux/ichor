package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo (see DataServices.kt); the wire format is documented in plans/data-services/README.md.

@Serializable
data class DragonflyStatus(
    val version: String = "",
    val error: String = "",
    val instances: List<DragonflyInstance> = emptyList(),
)

@Serializable
data class DragonflyInstance(
    val namespace: String = "",
    val name: String = "",
    /** The operator's own word: Ready, or a step such as a rolling update. */
    val phase: String = "",
    val health: String = "",
    /** Wire values of [DragonflyReason]. */
    val reasons: List<String> = emptyList(),
    val replicas: Int = 0,
    val readyPods: Int = 0,
    /** Pod with role=master, "" when none. */
    val master: String = "",
    /** The master first. */
    val pods: List<DragonflyPod> = emptyList(),
) {
    val serviceHealth: ServiceHealth get() = ServiceHealth.from(health)
    val label: String get() = "$namespace/$name"
    val reasonList: List<DragonflyReason> get() = reasons.mapNotNull(DragonflyReason::from)
}

@Serializable
data class DragonflyPod(
    val name: String,
    val node: String = "",
    val phase: String = "",
    /** The operator's role label: master or replica. */
    val role: String = "",
    val ready: Boolean = false,
)

/** Why a Dragonfly instance is not ok, as the Go core names it. */
enum class DragonflyReason(val wire: String) {
    NO_READY("noReady"),
    NO_MASTER("noMaster"),
    MASTERS("masters"),
    PODS("pods"),
    NOT_READY("notReady"),
    ;

    companion object {
        fun from(wire: String): DragonflyReason? = entries.firstOrNull { it.wire == wire }
    }
}
