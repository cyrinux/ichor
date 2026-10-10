package name.levis.ichor.model

import kotlinx.serialization.Serializable

/**
 * Every node's named volumes and disks of a Talos cluster, as the Go core's ClusterStorageHealth
 * reads them in one call (for the monitor's fill and SMART alerts).
 */
@Serializable
data class ClusterStorageHealth(
    val context: String = "",
    val nodes: List<NodeStorageHealth> = emptyList(),
)

/** One node's volumes and disks; [error] set (and both lists empty) when the node did not answer. */
@Serializable
data class NodeStorageHealth(
    val node: String,
    val hostname: String = "",
    val volumes: List<StorageVolumeHealth> = emptyList(),
    val disks: List<StorageDiskHealth> = emptyList(),
    val error: String? = null,
)

/** A volume's fill: [key] is "node|name", stable across checks. */
@Serializable
data class StorageVolumeHealth(
    val key: String,
    val name: String,
    val mount: String = "",
    val usedPercent: Double = 0.0,
    val freeBytes: Long = 0,
    val sizeBytes: Long = 0,
)

/** A disk's SMART verdict ([SMART_FAILING], "ok" or "unknown"): [key] is "node|smart|device". */
@Serializable
data class StorageDiskHealth(
    val key: String,
    val device: String,
    val model: String = "",
    val health: String = "",
    val reason: String = "",
) {
    companion object {
        const val SMART_FAILING = "failing"
    }
}
