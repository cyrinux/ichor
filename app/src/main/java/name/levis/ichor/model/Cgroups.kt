package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/talosmobile/cgroups.go.

/** PSI averages: % of the last 10/60 s some (or all) tasks waited for the resource. */
@Serializable
data class CgroupPsi(val some10: Double = 0.0, val some60: Double = 0.0, val full10: Double = 0.0, val full60: Double = 0.0)

@Serializable
data class CgroupPressure(val cpu: CgroupPsi = CgroupPsi(), val memory: CgroupPsi = CgroupPsi(), val io: CgroupPsi = CgroupPsi()) {
    /** The worst "some" 10 s average of the three, for a single dot per row. */
    val worst: Double get() = maxOf(cpu.some10, memory.some10, io.some10)
}

/** One cgroup; limits of 0 mean none, CPU (µs) and IO (bytes) are cumulative since boot. */
@Serializable
data class CgroupNode(
    val name: String,
    val kind: String = "group",
    val memCurrent: Long = 0,
    val memPeak: Long = 0,
    val memMax: Long = 0,
    val memHigh: Long = 0,
    val memLow: Long = 0,
    val memMin: Long = 0,
    val swapCurrent: Long = 0,
    val oomKills: Long = 0,
    val cpuUsec: Long = 0,
    val throttledUsec: Long = 0,
    val cpuWeight: Long = 0,
    val cpuLimit: Double = 0.0,
    val ioRead: Long = 0,
    val ioWrite: Long = 0,
    val pressure: CgroupPressure? = null,
    val children: List<CgroupNode> = emptyList(),
)

@Serializable
data class CgroupHotspot(val resource: String, val name: String, val parent: String = "", val some10: Double = 0.0)

@Serializable
data class CgroupAlert(val kind: String, val name: String, val parent: String = "", val count: Long = 0, val percent: Double = 0.0)

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
    val cpuPercent: Double,
    val ioPerSecond: Double,
) {
    val hasChildren: Boolean get() = node.children.isNotEmpty()
}

enum class CgroupSort { MEMORY, CPU, PRESSURE }

/**
 * The tree as rows, depth first: children of [expanded] paths only, siblings sorted by
 * [sort]. CPU% is of one core (like top) and IO is bytes read + written per second, both
 * between [previous] and [current]; 0 without a previous sample or when a counter went back.
 */
fun cgroupRows(previous: CgroupReport?, current: CgroupReport, expanded: Set<String>, sort: CgroupSort): List<CgroupRow> {
    val root = current.root ?: return emptyList()
    val seconds = previous?.let { (current.at - it.at) / 1000.0 } ?: 0.0
    val before = previous?.root?.let { cgroupsByPath(it) }.orEmpty()
    val rows = mutableListOf<CgroupRow>()

    fun add(node: CgroupNode, path: String, depth: Int) {
        val old = before[path]
        val cpu = if (seconds > 0 && old != null && node.cpuUsec >= old.cpuUsec) (node.cpuUsec - old.cpuUsec) / 1e6 / seconds * 100 else 0.0
        val io = if (seconds > 0 && old != null && node.ioRead + node.ioWrite >= old.ioRead + old.ioWrite) {
            (node.ioRead + node.ioWrite - old.ioRead - old.ioWrite) / seconds
        } else {
            0.0
        }
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
        CgroupSort.CPU -> children.sortedByDescending { (child, childPath) ->
            val old = before[childPath]
            if (seconds > 0 && old != null) child.cpuUsec - old.cpuUsec else child.cpuUsec
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

/** Paths open on first show: the top groups, so services and QoS classes are visible. */
fun defaultExpandedCgroups(report: CgroupReport): Set<String> =
    report.root?.children.orEmpty().filter { it.children.isNotEmpty() }.map { it.name }.toSet() +
        report.root?.children.orEmpty().filter { it.name == "kubepods" }
            .flatMap { k -> k.children.filter { it.kind == "group" }.map { childPath(k.name, it.name) } }
