import Foundation

// Mirrors go/ichorgo etcd_membership.go: the phase of Talos 1.15's EtcdMembershipStatus (same as Android).

/// The settled phase: a voting member.
public let etcdPhaseMember = "member"

extension EtcdNodeStatus {
    /// The node's etcd membership phase when it is not settled as a voting member (preparing,
    /// joining, learner, leaving, left); nil when it is, and on Talos 1.14 and older where there is none.
    public var unsettledPhase: String? {
        guard let phase = membershipPhase?.trimmingCharacters(in: .whitespaces), !phase.isEmpty,
            phase != etcdPhaseMember
        else {
            return nil
        }
        return phase
    }
}
