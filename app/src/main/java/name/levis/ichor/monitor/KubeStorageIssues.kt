package name.levis.ichor.monitor

import name.levis.ichor.model.HistoryVolumeEntry
import name.levis.ichor.model.KubeNodeDisk
import name.levis.ichor.model.KubeNodeDiskUsage
import name.levis.ichor.model.KubeNodesOverview

/** The volume name of a Kubernetes node's disk (the kubelet's nodefs) in its storage issue and history. */
const val KUBE_NODEFS = "nodefs"

/** The storage issue key of node [node]'s disk, as the Talos path keys a volume ("node|volume"). */
fun kubeStorageKey(node: String): String = "$node|$KUBE_NODEFS"

/** The nodes of [disk] worth judging: named, and measured by their kubelet or reporting DiskPressure. */
private fun KubeNodeDisk.readNodes(): List<KubeNodeDiskUsage> =
    nodes.filter { it.name.isNotBlank() && (it.measured || it.diskPressure) }

/**
 * The storage issues of a cluster added from a kubeconfig, from each node's disk as its kubelet
 * reports it, in the [StorageDetail] format of [storageIssuesOf] (volume [KUBE_NODEFS], host the
 * node name): used at [warn] % or more is a warning, at [critical] % or more critical, and a node
 * reporting DiskPressure is critical whatever its percent. Null when nothing could be read (the
 * read skipped on a large cluster, nodes/proxy refused, no kubelet answered): an absent reading,
 * never an alert nor an all-clear. A node of [listed] that did not answer keeps its [known] issues.
 */
fun kubeStorageIssuesOf(
    disk: KubeNodeDisk,
    warn: Int,
    critical: Int,
    listed: Set<String> = emptySet(),
    known: Map<String, String> = emptyMap(),
): Map<String, String>? {
    if (disk.skipped) return null
    val read = disk.readNodes()
    if (read.isEmpty() && (disk.forbidden || disk.measured == 0)) return null
    val warnAt = warn.coerceIn(STORAGE_WARN_RANGE)
    val criticalAt = critical.coerceIn(storageCriticalRange(warnAt))
    val answered = read.map { it.name }.toSet()
    val out = sortedMapOf<String, String>()
    out += known.filterKeys { key -> key.substringBefore('|').let { it in listed && it !in answered } }
    read.forEach { node ->
        val severity = when {
            node.diskPressure || node.usedPercent >= criticalAt -> DATA_CRITICAL
            node.usedPercent >= warnAt -> DATA_WARNING
            else -> return@forEach
        }
        out[kubeStorageKey(node.name)] = StorageDetail(
            severity, smart = false, node.name, KUBE_NODEFS, node.usedPercent.coerceIn(0, 100),
            node.availableBytes, node.capacityBytes, warnAt,
            reason = if (node.diskPressure) DISK_PRESSURE else "",
        ).format()
    }
    return out
}

private const val DISK_PRESSURE = "DiskPressure"

/** Every measured node's disk fill, for the history ring and the volume trends; null when [disk] is null. */
fun kubeHistoryVolumesOf(disk: KubeNodeDisk?): List<HistoryVolumeEntry>? =
    disk?.takeUnless { it.skipped }?.nodes?.filter { it.name.isNotBlank() && it.measured }?.map {
        HistoryVolumeEntry(kubeStorageKey(it.name), KUBE_NODEFS, it.name, it.usedPercent.toDouble())
    }

/** A kubeconfig cluster's history detail ([historyDetailOf]) with each node's disk of [disk]. */
fun historyDetailOf(nodes: KubeNodesOverview, disk: KubeNodeDisk?): HistoryDetail =
    historyDetailOf(nodes).copy(volumes = kubeHistoryVolumesOf(disk))
