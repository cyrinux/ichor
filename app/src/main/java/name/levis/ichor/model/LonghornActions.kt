package name.levis.ichor.model

/** Actions on a Longhorn volume or node, by their Go core name. */
enum class LonghornAction(val wire: String) {
    /** Snapshot the volume and back the snapshot up to its target. */
    BACKUP("backup"),
    /** Give the space freed in the volume's filesystem back to Longhorn. */
    TRIM("trim"),
    /** Set the volume's replica count. */
    REPLICAS("replicas"),
    SCHEDULING_ON("schedulingOn"),
    SCHEDULING_OFF("schedulingOff"),
    /** Move every replica off the node; scheduling goes off. */
    EVICT("evict"),
    CANCEL_EVICTION("cancelEviction"),
}

/** Longhorn refuses more replicas than this. */
const val LONGHORN_MAX_REPLICAS = 20

/** What can be done to the volume now: backups and trims need it attached. */
val LonghornVolume.actions: List<LonghornAction>
    get() = listOfNotNull(
        LonghornAction.BACKUP.takeIf { attached && !backingUp },
        LonghornAction.TRIM.takeIf { attached },
        LonghornAction.REPLICAS,
    )

/** What can be done to the node now. */
val LonghornNode.actions: List<LonghornAction>
    get() = listOfNotNull(
        if (allowScheduling) LonghornAction.SCHEDULING_OFF else LonghornAction.SCHEDULING_ON,
        if (evictionRequested) LonghornAction.CANCEL_EVICTION else LonghornAction.EVICT,
    )

/**
 * Replica counts worth offering for [volume]: one per node at most (Longhorn keeps replicas of
 * a volume on different nodes by default), never hiding the current count.
 */
fun LonghornStatus.replicaChoices(volume: LonghornVolume): IntRange =
    1..maxOf(nodes.size, volume.replicasDesired, 1).coerceAtMost(LONGHORN_MAX_REPLICAS)

/** Key of an action in flight on a volume or node. */
fun longhornActionKey(namespace: String, name: String) = "$namespace/$name"
