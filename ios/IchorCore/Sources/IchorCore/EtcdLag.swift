import Foundation

/// Raft entries a member may trail by before it counts as lagging. Members are probed one call
/// each, not at the same instant, so a busy but healthy cluster shows a small gap (same as Android).
public let etcdLagEntries: UInt64 = 1_000

/// How far a member trails: `behindLeader` committed entries it has not received yet (nil when
/// the leader did not answer), `applyBacklog` entries it committed but not yet applied.
public struct EtcdLag: Equatable, Sendable {
    public let behindLeader: UInt64?
    public let applyBacklog: UInt64

    public var lagging: Bool { (behindLeader ?? 0) >= etcdLagEntries || applyBacklog >= etcdLagEntries }
}

/// The lag of `status` against the leader among `statuses`; nil when the member did not answer.
public func etcdLag(_ status: EtcdNodeStatus, in statuses: [EtcdNodeStatus]) -> EtcdLag? {
    guard status.error == nil else { return nil }
    let leader = statuses.first { $0.isLeader && $0.error == nil }
    let behind = leader.map { $0.raftIndex > status.raftIndex ? $0.raftIndex - status.raftIndex : 0 }
    // A missing or 0 applied index is "not reported" (older cores), not a full backlog.
    let applied = status.raftAppliedIndex ?? 0
    let backlog = applied > 0 && status.raftIndex > applied ? status.raftIndex - applied : 0
    return EtcdLag(behindLeader: behind, applyBacklog: backlog)
}

/// The members of `statuses` that trail the leader or their own commits.
public func laggingMembers(_ statuses: [EtcdNodeStatus]) -> [EtcdNodeStatus] {
    statuses.filter { etcdLag($0, in: statuses)?.lagging == true }
}
