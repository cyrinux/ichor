package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/cgroups.go.

/** PSI averages: % of the last 10/60 s some (or all) tasks waited for the resource. */
@Serializable
data class CgroupPsi(val some10: Double = 0.0, val some60: Double = 0.0, val full10: Double = 0.0, val full60: Double = 0.0)

@Serializable
data class CgroupPressure(val cpu: CgroupPsi = CgroupPsi(), val memory: CgroupPsi = CgroupPsi(), val io: CgroupPsi = CgroupPsi()) {
    /** The worst "some" 10 s average of the three, for a single dot per row. */
    val worst: Double get() = maxOf(cpu.some10, memory.some10, io.some10)
}

/**
 * One cgroup; a memMax of 0 means no limit, CPU (µs) and IO (bytes) are cumulative since
 * boot. The tree stops at pods: their containers are the Pods tab's.
 */
@Serializable
data class CgroupNode(
    val name: String,
    val kind: String = "group",
    val memCurrent: Long = 0,
    val memMax: Long = 0,
    val oomKills: Long = 0,
    val cpuUsec: Long = 0,
    val ioRead: Long = 0,
    val ioWrite: Long = 0,
    val pressure: CgroupPressure? = null,
    val children: List<CgroupNode> = emptyList(),
)

/** "name (parent)": Talos has both system/runtime and podruntime/runtime. */
private fun who(name: String, parent: String) = if (parent.isBlank()) name else "$name ($parent)"

@Serializable
data class CgroupHotspot(val resource: String, val name: String, val parent: String = "", val some10: Double = 0.0) {
    val who: String get() = who(name, parent)
}

@Serializable
data class CgroupAlert(val kind: String, val name: String, val parent: String = "", val count: Long = 0, val percent: Double = 0.0) {
    val who: String get() = who(name, parent)
}

@Serializable
data class CgroupReport(
    val at: Long,
    val pressure: CgroupPressure = CgroupPressure(),
    val hotspots: List<CgroupHotspot> = emptyList(),
    val alerts: List<CgroupAlert> = emptyList(),
    val root: CgroupNode? = null,
)

enum class PressureLevel { OK, WARN, BAD }

/**
 * Level of a PSI "some" avg10: below 5% tasks barely wait; from 20% the node is visibly
 * slowed (the usual thresholds of PSI-based alerting).
 */
fun pressureLevel(some10: Double): PressureLevel = when {
    some10 >= PRESSURE_BAD -> PressureLevel.BAD
    some10 >= PRESSURE_WARN -> PressureLevel.WARN
    else -> PressureLevel.OK
}

const val PRESSURE_WARN = 5.0
const val PRESSURE_BAD = 20.0

/** A cgroup in the flattened tree, with its CPU and IO rates since the previous sample. */
data class CgroupRow(
    val path: String,
    val depth: Int,
    val node: CgroupNode,
    /** Percent of one core (like top); null without a usable previous sample. */
    val cpuPercent: Double?,
    /** Bytes read + written per second; null without a usable previous sample. */
    val ioPerSecond: Double?,
) {
    val hasChildren: Boolean get() = node.children.isNotEmpty()
}

enum class CgroupSort { MEMORY, CPU, PRESSURE }

/**
 * The tree as rows, depth first: children of [expanded] paths only, siblings sorted by
 * [sort]. CPU% is of one core (like top) and IO is bytes read + written per second, both
 * between [previous] and [current]; null without a previous sample or when a counter went back.
 */
fun cgroupRows(previous: CgroupReport?, current: CgroupReport, expanded: Set<String>, sort: CgroupSort): List<CgroupRow> {
    val root = current.root ?: return emptyList()
    val seconds = previous?.let { (current.at - it.at) / 1000.0 } ?: 0.0
    val before = previous?.root?.let { cgroupsByPath(it) }.orEmpty()
    val rows = mutableListOf<CgroupRow>()

    fun rate(now: Long, old: Long?): Double? =
        if (seconds > 0 && old != null && now >= old) (now - old) / seconds else null

    fun add(node: CgroupNode, path: String, depth: Int) {
        val old = before[path]
        val cpu = rate(node.cpuUsec, old?.cpuUsec)?.let { it / 1e6 * 100 }
        val io = rate(node.ioRead + node.ioWrite, old?.let { it.ioRead + it.ioWrite })
        val row = CgroupRow(path, depth, node, cpu, io)
        rows += row
        if (path in expanded) {
            sortedChildren(node, path, sort, before, seconds).forEach { (child, childPath) -> add(child, childPath, depth + 1) }
        }
    }

    // The root (".") is the whole node, which the pressure card already shows: start below it.
    sortedChildren(root, "", sort, before, seconds).forEach { (child, path) -> add(child, path, 0) }
    return rows
}

private fun sortedChildren(
    node: CgroupNode,
    path: String,
    sort: CgroupSort,
    before: Map<String, CgroupNode>,
    seconds: Double,
): List<Pair<CgroupNode, String>> {
    val children = node.children.map { it to childPath(path, it.name) }
    return when (sort) {
        CgroupSort.MEMORY -> children.sortedByDescending { it.first.memCurrent }
        CgroupSort.PRESSURE -> children.sortedByDescending { it.first.pressure?.worst ?: 0.0 }
        // CPU since the previous sample; without one, nothing to compare (not the since-boot total).
        CgroupSort.CPU -> children.sortedByDescending { (child, childPath) ->
            val old = before[childPath]
            if (seconds > 0 && old != null && child.cpuUsec >= old.cpuUsec) child.cpuUsec - old.cpuUsec else 0L
        }
    }
}

private fun childPath(parent: String, name: String) = if (parent.isEmpty()) name else "$parent\u0000$name"

fun cgroupsByPath(root: CgroupNode): Map<String, CgroupNode> {
    val out = mutableMapOf<String, CgroupNode>()
    fun walk(node: CgroupNode, path: String) {
        node.children.forEach { child ->
            val p = childPath(path, child.name)
            out[p] = child
            walk(child, p)
        }
    }
    walk(root, "")
    return out
}

/**
 * Paths open on first show: the Talos groups (system, podruntime), so their services are
 * visible. kubepods stays closed: its pods are the Pods tab's, open it to compare them.
 */
fun defaultExpandedCgroups(report: CgroupReport): Set<String> =
    report.root?.children.orEmpty().filter { it.children.isNotEmpty() && it.name != "kubepods" }.map { it.name }.toSet()
