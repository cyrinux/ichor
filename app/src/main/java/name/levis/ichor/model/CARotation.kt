package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/rotateca_plan.go and rotateca_run.go.

/** A CA certificate: its subject, validity (unix seconds) and the start of its SHA-256. */
@Serializable
data class CARotationCA(
    val subject: String = "",
    val notBefore: Long = 0,
    val notAfter: Long = 0,
    val fingerprint: String = "",
)

/** A node the rotation patches; [problem] says why it stands in the way ("" when it does not). */
@Serializable
data class CARotationNode(
    val node: String = "",
    val hostname: String = "",
    val role: String = "",
    val version: String = "",
    val reachable: Boolean = false,
    val acceptedCAs: Int = 0,
    val problem: String = "",
) {
    val health: ServiceHealth get() = if (problem.isEmpty()) ServiceHealth.OK else ServiceHealth.CRITICAL
}

/** What TalosCARotationPlan says: the CA, the nodes, and what stands in the way. */
@Serializable
data class CARotationPlan(
    val context: String = "",
    val endpoints: List<String> = emptyList(),
    val ca: CARotationCA? = null,
    val nodes: List<CARotationNode> = emptyList(),
    /** The nodes are every member cluster discovery lists, not only the talosconfig's. */
    val discovered: Boolean = false,
    val blockers: List<String> = emptyList(),
    val warnings: List<String> = emptyList(),
    val ready: Boolean = false,
)

@Serializable
data class CARotationProgressNode(
    val node: String = "",
    val hostname: String = "",
    val role: String = "",
    val state: String = "",
)

/** One event of StartTalosCARotation. */
@Serializable
data class CARotationProgress(
    val phase: String = "",
    val step: Int = 0,
    val total: Int = 0,
    val node: String = "",
    val hostname: String = "",
    val message: String = "",
    val at: Long = 0,
    val nodes: List<CARotationProgressNode> = emptyList(),
)

/** The phases of a rotation, in order; [DONE] ends it. */
enum class CARotationPhase(val wire: String) {
    CHECK("check"),
    ACCEPT("accept"),
    HANDOVER("handover"),
    SWAP("swap"),
    DROP("drop"),
    DONE("done"),
    ;

    companion object {
        /** The phases a timeline shows (DONE is not a step). */
        val STEPS = entries.filter { it != DONE }

        fun of(wire: String): CARotationPhase? = entries.firstOrNull { it.wire == wire }
    }
}

/** A node's state within the current phase. */
object CARotationNodeState {
    const val PENDING = "pending"
    const val PATCHING = "patching"
    const val VERIFYING = "verifying"
    const val DONE = "done"
    const val FAILED = "failed"
}

data class CARotationStep(val phase: CARotationPhase, val status: StepStatus, val at: Long = 0, val message: String = "")

/**
 * The rotation's timeline from the [events] so far: phases before the latest are done, the
 * latest is current (failed when [failed]), the rest pending; all done once [finished]
 * without failure. A step's message is its last event's.
 */
fun caRotationTimeline(events: List<CARotationProgress>, finished: Boolean, failed: Boolean): List<CARotationStep> {
    val known = events.mapNotNull { e -> CARotationPhase.of(e.phase)?.let { it to e } }
    val reached = known.maxOfOrNull { it.first.ordinal } ?: if (failed) 0 else -1
    return CARotationPhase.STEPS.map { phase ->
        val own = known.filter { it.first == phase }.map { it.second }
        val status = when {
            finished && !failed -> StepStatus.DONE
            phase.ordinal < reached -> StepStatus.DONE
            phase.ordinal == reached && failed -> StepStatus.FAILED
            phase.ordinal == reached -> StepStatus.CURRENT
            else -> StepStatus.PENDING
        }
        CARotationStep(phase, status, own.firstOrNull()?.at ?: 0, own.lastOrNull()?.message.orEmpty())
    }
}

/** How a progress node's [CARotationProgressNode.state] reads as health. */
fun caNodeHealth(state: String): ServiceHealth = when (state) {
    CARotationNodeState.DONE -> ServiceHealth.OK
    CARotationNodeState.FAILED -> ServiceHealth.CRITICAL
    CARotationNodeState.PATCHING, CARotationNodeState.VERIFYING -> ServiceHealth.WARNING
    else -> ServiceHealth.IDLE
}
