package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo resetPlan (NodeResetPlan) and the wipe modes of NodeReset.

@Serializable
data class NodeResetMember(val id: String = "", val healthy: Boolean = false)

/** What resetting a node would wipe and leave; any [blockers] forbids it. */
@Serializable
data class NodeResetPlan(
    val node: String = "",
    val hostname: String = "",
    /** "controlplane" or "worker". */
    val role: String = "",
    /** The node's etcd member; null for a worker or a control plane outside etcd. */
    val etcdMember: NodeResetMember? = null,
    val lastControlPlane: Boolean = false,
    val blockers: List<String> = emptyList(),
    val warnings: List<String> = emptyList(),
    /** The disks other than the system disk, as /dev paths. */
    val userDisks: List<String> = emptyList(),
)

val NodeResetPlan.allowed: Boolean get() = blockers.isEmpty()

/** What a reset wipes; [wire] is NodeReset's wipe argument. */
enum class ResetWipe(val wire: String) {
    /** The system disk and the user disks of the plan. */
    ALL("all"),
    /** The system disk only: Talos installs again on the next boot. */
    SYSTEM("system"),
    /** The user disks of the plan only. */
    USER("user"),
}

/** The wipe modes the plan offers: without user disks, "everything" is the system disk alone. */
val NodeResetPlan.wipeModes: List<ResetWipe>
    get() = if (userDisks.isEmpty()) listOf(ResetWipe.SYSTEM) else ResetWipe.entries

/** A reset that does not leave etcd first leaves a dead member the others must remove. */
fun NodeResetPlan.leavesDeadMember(graceful: Boolean): Boolean = !graceful && etcdMember != null

data class ResetRequest(
    val wipe: ResetWipe = ResetWipe.SYSTEM,
    /** Leave etcd and drain the node first (`--graceful`). */
    val graceful: Boolean = true,
    /** Restart the node afterwards instead of powering it off (`--reboot`). */
    val reboot: Boolean = true,
)
