package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/talosmobile etcdMemberPlan, etcdForfeitLeadership and etcdRemoveMember.

@Serializable
data class EtcdMemberRef(val id: String = "", val hostname: String = "")

/** What to type to confirm acting on the member: its hostname, or its id when it has none. */
val EtcdMemberRef.confirmToken: String get() = hostname.ifBlank { id }

/** What removing a member would leave behind; any [blockers] forbids it. */
@Serializable
data class EtcdMemberPlan(
    val member: EtcdMemberRef = EtcdMemberRef(),
    val healthyAfter: Int = 0,
    val membersAfter: Int = 0,
    /** Whether the remaining healthy members still form a quorum. */
    val quorumAfter: Boolean = false,
    val blockers: List<String> = emptyList(),
    val warnings: List<String> = emptyList(),
)

val EtcdMemberPlan.allowed: Boolean get() = blockers.isEmpty()

@Serializable
data class EtcdForfeitResult(
    /** The new leader; empty when the node was not the leader. */
    val member: String = "",
)

/**
 * The node to ask for removing [memberId]: another member that answered (never the member
 * itself, which may be gone), preferring one without etcd errors, the leader last. Null when no other member is reachable.
 */
fun removalNode(statuses: List<EtcdNodeStatus>, memberId: String): String? =
    statuses.filter { it.error == null && it.memberId.isNotEmpty() && it.memberId != memberId }
        .sortedWith(compareBy<EtcdNodeStatus> { it.errors.isNotEmpty() }.thenBy { it.isLeader })
        .firstOrNull()?.node
