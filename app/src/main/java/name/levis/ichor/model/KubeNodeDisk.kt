package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/kube_node_disk.go.

/**
 * Each node's disk as its kubelet reports it (Go `KubeNodeDisk`), for the background monitor of a
 * cluster added from a kubeconfig. A node whose kubelet did not answer is left out, never reported
 * as full. [forbidden]: nodes/proxy was refused; [measured]: how many kubelets answered;
 * [skipped]: the cluster has too many nodes to ask on every check.
 */
@Serializable
data class KubeNodeDisk(
    val nodes: List<KubeNodeDiskUsage> = emptyList(),
    val forbidden: Boolean = false,
    val measured: Int = 0,
    val skipped: Boolean = false,
)

/** One node's nodefs ([usedPercent], [availableBytes] of [capacityBytes]) and whether it reports DiskPressure. */
@Serializable
data class KubeNodeDiskUsage(
    val name: String = "",
    val usedPercent: Int = 0,
    val availableBytes: Long = 0,
    val capacityBytes: Long = 0,
    val imageFsUsedPercent: Int = 0,
    val diskPressure: Boolean = false,
) {
    /** The kubelet reported a size: its percent means something. */
    val measured: Boolean get() = capacityBytes > 0
}
