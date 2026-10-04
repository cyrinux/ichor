package name.levis.ichor.model

/**
 * Raft entries a member may trail by before it counts as lagging. Members are probed one
 * call each, not at the same instant, so a busy but healthy cluster shows a small gap.
 */
const val ETCD_LAG_ENTRIES = 1_000L

/**
 * How far a member trails: [behindLeader] committed entries it has not received yet (null
 * when the leader did not answer), [applyBacklog] entries it committed but not yet applied.
 */
data class EtcdLag(val behindLeader: Long?, val applyBacklog: Long) {
    val lagging: Boolean get() = (behindLeader ?: 0) >= ETCD_LAG_ENTRIES || applyBacklog >= ETCD_LAG_ENTRIES
}

/** The lag of [status] against the leader among [statuses]; null when the member did not answer. */
fun etcdLag(status: EtcdNodeStatus, statuses: List<EtcdNodeStatus>): EtcdLag? {
    if (status.error != null) return null
    val leader = statuses.firstOrNull { it.isLeader && it.error == null }
    val behind = leader?.let { (it.raftIndex - status.raftIndex).coerceAtLeast(0) }
    // An applied index of 0 is "not reported" (older cores), not a full backlog.
    val backlog = if (status.raftAppliedIndex > 0) (status.raftIndex - status.raftAppliedIndex).coerceAtLeast(0) else 0
    return EtcdLag(behind, backlog)
}

/** The members of [statuses] that trail the leader or their own commits. */
fun laggingMembers(statuses: List<EtcdNodeStatus>): List<EtcdNodeStatus> =
    statuses.filter { etcdLag(it, statuses)?.lagging == true }
