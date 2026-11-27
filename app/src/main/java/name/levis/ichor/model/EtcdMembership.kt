package name.levis.ichor.model

// Mirrors go/ichorgo etcd_membership.go: the phase of Talos 1.15's EtcdMembershipStatus.

/** The settled phase: a voting member. */
const val ETCD_PHASE_MEMBER = "member"

/**
 * The node's etcd membership phase when it is not settled as a voting member (preparing, joining,
 * learner, leaving, left); null when it is, and on Talos 1.14 and older where there is none.
 */
val EtcdNodeStatus.unsettledPhase: String?
    get() = membershipPhase?.trim()?.takeIf { it.isNotEmpty() && it != ETCD_PHASE_MEMBER }
