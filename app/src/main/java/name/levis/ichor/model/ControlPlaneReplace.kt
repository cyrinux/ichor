package name.levis.ichor.model

import java.net.URI
import kotlinx.serialization.Serializable

// Mirrors go/ichorgo cpReplacePlan and cpReplaceWait (ControlPlaneReplacePlan / ControlPlaneReplaceWait).

@Serializable
data class CpReplaceMember(
    val id: String = "",
    val hostname: String = "",
    val node: String = "",
    /** False once the member is removed from etcd. */
    val found: Boolean = false,
    val healthy: Boolean = false,
    /** Whether the node's Talos API still answers (it can be reset). */
    val reachable: Boolean = false,
)

@Serializable
data class CpReplaceQuorum(
    val members: Int = 0,
    val healthy: Int = 0,
    val afterRemoval: Int = 0,
    val healthyAfter: Int = 0,
    val safe: Boolean = false,
)

@Serializable
data class CpReplaceNode(val id: String = "", val node: String = "", val hostname: String = "")

@Serializable
data class CpReplaceStep(val id: String = "", val state: String = "", val detail: String = "")

@Serializable
data class CpReplacePlan(
    val member: CpReplaceMember = CpReplaceMember(),
    val quorum: CpReplaceQuorum = CpReplaceQuorum(),
    val leader: CpReplaceNode = CpReplaceNode(),
    val steps: List<CpReplaceStep> = emptyList(),
    /** The healthy control plane whose machine config the new node copies; blank node: none. */
    val template: CpReplaceNode = CpReplaceNode(),
)

@Serializable
data class CpReplaceWait(
    val joined: Boolean = false,
    val members: List<CpReplaceMember> = emptyList(),
    val detail: String = "",
)

/** The five steps of a replacement, in order. */
enum class CpStep(val wire: String) {
    CONFIRM_QUORUM("confirmQuorum"),
    REMOVE_MEMBER("removeMember"),
    RESET_OR_POWER_OFF("resetOrPowerOff"),
    BOOT_NEW_NODE("bootNewNode"),
    WAIT_MEMBER("waitMember"),
}

enum class CpStepState(val wire: String) {
    PENDING("pending"),
    READY("ready"),
    DONE("done"),
    SKIPPED("skipped"),
    BLOCKED("blocked"),
    ;

    companion object {
        /** Unknown states read as pending: never enable an action Go did not mark ready. */
        fun of(wire: String): CpStepState = entries.firstOrNull { it.wire == wire } ?: PENDING
    }
}

/** The step [step] of the plan; pending when Go did not list it. */
fun CpReplacePlan.step(step: CpStep): CpReplaceStep = steps.firstOrNull { it.id == step.wire } ?: CpReplaceStep(step.wire, CpStepState.PENDING.wire)

fun CpReplacePlan.state(step: CpStep): CpStepState = CpStepState.of(step(step).state)

/** The member to type when confirming its removal: hostname, else id. */
val CpReplacePlan.memberRef: EtcdMemberRef get() = EtcdMemberRef(member.id, member.hostname)

/** The removal plan in the shape of the etcd screen's confirmation. */
val CpReplacePlan.removalPlan: EtcdMemberPlan
    get() = EtcdMemberPlan(
        member = memberRef,
        healthyAfter = quorum.healthyAfter,
        membersAfter = quorum.afterRemoval,
        quorumAfter = quorum.safe,
        blockers = step(CpStep.REMOVE_MEMBER).takeIf { CpStepState.of(it.state) == CpStepState.BLOCKED }?.detail?.let(::listOf).orEmpty(),
    )

/**
 * The voting members to wait past for the new one: those left once the member is removed. A
 * plan read after the removal already counts without it.
 */
val CpReplacePlan.membersBeforeJoin: Int get() = if (member.found) quorum.afterRemoval else quorum.members

/** The line to run from a laptop on the new node, booted in maintenance mode. */
const val APPLY_CONFIG_COMMAND = "talosctl apply-config --insecure -n <new-node-ip> -f controlplane.yaml"

/**
 * The members a replacement is offered for: voting members with no healthy status (their node
 * did not answer, or its etcd reports errors). Learners are catching up, not failed.
 */
fun EtcdOverview.replaceCandidates(): List<EtcdMember> = members.filter { m ->
    !m.isLearner && m.id.isNotEmpty() && statuses.none { it.memberId == m.id && it.error == null && it.errors.isEmpty() }
}

/** The member's node address, from its client then peer URLs; blank when none parses. */
val EtcdMember.address: String
    get() = (clientUrls + peerUrls).firstNotNullOfOrNull { url ->
        runCatching { URI(url).host }.getOrNull()?.removePrefix("[")?.removeSuffix("]")?.takeIf { it.isNotBlank() }
    }.orEmpty()
