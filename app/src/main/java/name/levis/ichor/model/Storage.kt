package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo nodeMounts, nodeVolumes, nodeDiskUsage and nodeDiskHealth.

@Serializable
data class MountList(val mounts: List<Mount> = emptyList())

@Serializable
data class Mount(
    val filesystem: String = "",
    val mountedOn: String = "",
    val size: Long = 0,
    val available: Long = 0,
    val used: Long = 0,
    val usedPercent: Double = 0.0,
)

/** How full a filesystem is: warn from 80 %, bad from 90 %. */
enum class UsageLevel { OK, WARN, BAD }

const val USAGE_WARN_PERCENT = 80.0
const val USAGE_BAD_PERCENT = 90.0

fun usageLevel(usedPercent: Double): UsageLevel = when {
    usedPercent >= USAGE_BAD_PERCENT -> UsageLevel.BAD
    usedPercent >= USAGE_WARN_PERCENT -> UsageLevel.WARN
    else -> UsageLevel.OK
}

/**
 * The mounts worth showing first, fullest first: real devices with a size, without the
 * per-pod kubelet mounts (a node reports hundreds of mounts).
 */
fun List<Mount>.shortList(): List<Mount> =
    filter { it.filesystem.startsWith("/dev/") && it.size > 0 && !it.mountedOn.startsWith("/var/lib/kubelet/") }
        .sortedByDescending { it.usedPercent }

val Mount.level: UsageLevel get() = usageLevel(usedPercent)

/** Bar fraction in [0, 1]. */
val Mount.usedFraction: Float get() = (usedPercent / 100.0).toFloat().coerceIn(0f, 1f)

@Serializable
data class VolumeList(
    val supported: Boolean = true,
    val reason: String = "",
    val volumes: List<Volume> = emptyList(),
)

@Serializable
data class Volume(
    val id: String,
    val phase: String = "",
    val type: String = "",
    val location: String = "",
    val size: Long = 0,
    val filesystem: String = "",
    val encryption: String = "",
    val mountedOn: String = "",
    val error: String? = null,
)

@Serializable
data class DiskUsage(
    val entries: List<DiskUsageEntry> = emptyList(),
    /** More entries than the core returns: the smallest were dropped. */
    val truncated: Boolean = false,
)

@Serializable
data class DiskUsageEntry(val path: String, val size: Long = 0, val isDir: Boolean = false, val error: String? = null)

/**
 * Start paths offered by the disk usage explorer, quickest first. Nothing is measured until
 * one is chosen: the node walks the whole tree under the path, and /var of a control plane
 * takes minutes.
 */
val DISK_USAGE_SHORTCUTS = listOf("/var/log", "/var/lib/etcd", "/etc", "/opt", "/system/state", "/var/lib", "/var", "/")

/** One explorer row: [fraction] is the share of the largest row. */
data class DiskUsageRow(
    val path: String,
    val name: String,
    val size: Long,
    val isDir: Boolean,
    val fraction: Float,
    val error: String? = null,
)

/** "/var/lib/" → "/var/lib"; "" and "/" → "/". */
fun normalizePath(path: String): String = "/" + path.split('/').filter { it.isNotEmpty() }.joinToString("/")

/**
 * The children of [root] among [entries] (which may include [root] itself, with the total),
 * biggest first then by name.
 */
fun diskUsageRows(entries: List<DiskUsageEntry>, root: String): List<DiskUsageRow> {
    val base = normalizePath(root)
    val children = entries
        .map { it.copy(path = normalizePath(it.path)) }
        .filter { it.path != base }
        .distinctBy { it.path }
    val largest = children.maxOfOrNull { it.size }?.coerceAtLeast(1) ?: 1
    return children
        .sortedWith(compareByDescending<DiskUsageEntry> { it.size }.thenBy { it.path })
        .map { e ->
            val name = if (base != "/" && e.path.startsWith("$base/")) e.path.removePrefix("$base/") else e.path.removePrefix("/")
            DiskUsageRow(e.path, name, e.size, e.isDir, (e.size.toFloat() / largest).coerceIn(0f, 1f), e.error)
        }
}

/** Size of [root] itself when [entries] reports it, else the sum of its direct children. */
fun diskUsageTotal(entries: List<DiskUsageEntry>, root: String): Long {
    val base = normalizePath(root)
    entries.firstOrNull { normalizePath(it.path) == base }?.let { return it.size }
    return diskUsageRows(entries, root).filter { '/' !in it.name }.sumOf { it.size }
}

/** A breadcrumb step: [label] ("/" or a directory name) leading to [path]. */
data class Crumb(val label: String, val path: String)

/** "/var/lib" → [/ → "/", var → "/var", lib → "/var/lib"]. */
fun breadcrumbs(path: String): List<Crumb> {
    val parts = normalizePath(path).split('/').filter { it.isNotEmpty() }
    return listOf(Crumb("/", "/")) + parts.indices.map { i -> Crumb(parts[i], "/" + parts.take(i + 1).joinToString("/")) }
}

@Serializable
data class DiskHealthReport(
    val supported: Boolean = true,
    val reason: String = "",
    val disks: List<DiskHealth> = emptyList(),
)

@Serializable
data class DiskHealth(
    /** Kernel name: "sda", "nvme0n1". */
    val device: String = "",
    val model: String = "",
    /** What the disk says about its state, when it says something. */
    val message: String = "",
    val serial: String = "",
    /** null: the disk does not report its health. */
    val healthy: Boolean? = null,
    val temperatureC: Long? = null,
    val powerOnHours: Long? = null,
    /** Life used, 0-100 (NVMe may report more). */
    val wearPercent: Long? = null,
    val criticalWarnings: List<String> = emptyList(),
    val attributes: List<DiskAttribute> = emptyList(),
)

@Serializable
data class DiskAttribute(val k: String = "", val v: String = "")

enum class DiskVerdict { HEALTHY, FAILING, UNKNOWN }

/** A disk with critical warnings is failing even if it still claims to be healthy. */
val DiskHealth.verdict: DiskVerdict
    get() = when {
        healthy == false || criticalWarnings.isNotEmpty() -> DiskVerdict.FAILING
        healthy == true -> DiskVerdict.HEALTHY
        else -> DiskVerdict.UNKNOWN
    }
