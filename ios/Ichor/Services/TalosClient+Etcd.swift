import Foundation
import IchorCore
import Ichorgo

/// The guided replacement of a failed control plane: the plan and the wait for the new member.
/// The steps' mutations are the existing etcdRemoveMember and reset calls.
extension TalosClient {
    /// Where replacing the failed etcd member stands, from live state (os:reader). node: the
    /// member's address as last seen, which names the old node once the member is removed.
    func controlPlaneReplacePlan(memberID: String, node: String) async throws -> CpReplacePlan {
        try await Self.json { [config, context] in IchorgoControlPlaneReplacePlan(config, context, memberID, node, $0) }
    }

    /// Removes the failed member as the replacement's step 2 (os:admin): Go reads the plan
    /// again and refuses a member that recovered or a removal that loses quorum.
    func controlPlaneReplaceRemove(memberID: String) async throws {
        try await Self.run { [config, context] error -> Void in
            _ = IchorgoControlPlaneReplaceRemove(config, context, memberID, error)
        }
    }

    /// Polls etcd up to timeout seconds until it has more healthy voting members than
    /// membersBefore (os:reader).
    func controlPlaneReplaceWait(membersBefore: Int, timeout: Int) async throws -> CpReplaceWait {
        try await Self.json { [config, context] in IchorgoControlPlaneReplaceWait(config, context, membersBefore, timeout, $0) }
    }
}
