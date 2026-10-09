package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo (see DataServices.kt).

@Serializable
data class LonghornStatus(
    val version: String = "",
    /** Installed but could not be read. */
    val error: String = "",
    val volumes: List<LonghornVolume> = emptyList(),
    val nodes: List<LonghornNode> = emptyList(),
    val backupTargets: List<LonghornBackupTarget> = emptyList(),
)

@Serializable
data class LonghornVolume(
    val name: String,
    val namespace: String = "",
    /** "" when no claim is bound. */
    val pvcNamespace: String = "",
    val pvcName: String = "",
    /** creating, attached, detached, attaching, detaching or deleting. */
    val state: String = "",
    /** healthy, degraded, faulted or unknown. */
    val robustness: String = "",
    val health: String = "",
    val replicasDesired: Int = 0,
    val replicasHealthy: Int = 0,
    val rebuilding: Int = 0,
    /** Nodes holding a replica, failed ones too. */
    val replicaNodes: List<String> = emptyList(),
    /** Where it is attached. */
    val node: String = "",
    val size: Long = 0,
    val actualSize: Long = 0,
    /** Unix millis, 0 when never. */
    val lastBackupAt: Long = 0,
    /** Slowest replica rebuild, 0-100, with [rebuilding] > 0. */
    val rebuildProgress: Int = 0,
    val backingUp: Boolean = false,
    val backupProgress: Int = 0,
    val restoring: Boolean = false,
    val restoreProgress: Int = 0,
    /** Why a replica cannot be placed (English, from Longhorn), "" when it can. */
    val scheduleError: String = "",
    /** Close to Longhorn's snapshot limit. */
    val tooManySnapshots: Boolean = false,
) {
    val serviceHealth: ServiceHealth get() = ServiceHealth.from(health)

    val attached: Boolean get() = state == "attached"

    /** The claim it backs ("namespace/name"), or the volume's own name when unbound. */
    val label: String get() = if (pvcName.isNotEmpty()) "$pvcNamespace/$pvcName" else name
}

@Serializable
data class LonghornNode(
    val name: String,
    /** Longhorn's own, where the node object lives. */
    val namespace: String = "",
    val ready: Boolean = false,
    val schedulable: Boolean = false,
    /** What the user asked: new replicas on the node, its replicas moved away. */
    val allowScheduling: Boolean = false,
    val evictionRequested: Boolean = false,
    /** Replicas it holds, failed ones too. */
    val replicas: Int = 0,
    val disks: List<LonghornDisk> = emptyList(),
)

@Serializable
data class LonghornDisk(
    val path: String = "",
    val schedulable: Boolean = false,
    val available: Long = 0,
    val maximum: Long = 0,
    val scheduled: Long = 0,
)

@Serializable
data class LonghornBackupTarget(
    val name: String = "",
    val url: String = "",
    val available: Boolean = false,
    val message: String = "",
)
