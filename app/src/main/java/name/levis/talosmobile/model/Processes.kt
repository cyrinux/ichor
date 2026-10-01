package name.levis.talosmobile.model

import kotlinx.serialization.Serializable

// Mirrors go/talosmobile/processes.go.

@Serializable
data class ProcessSample(val at: Long, val processes: List<ProcessInfo> = emptyList())

@Serializable
data class ProcessInfo(
    val pid: Int,
    val ppid: Int = 0,
    val state: String = "",
    val threads: Int = 0,
    /** Cumulative CPU time in seconds. */
    val cpuTime: Double = 0.0,
    val rss: Long = 0,
    val vms: Long = 0,
    val command: String = "",
    val args: String = "",
)

/** A process with its CPU usage since the previous sample. */
data class ProcessRow(val info: ProcessInfo, val cpuPercent: Double)

enum class ProcessSort { CPU, MEMORY }

/**
 * CPU% per pid between two samples, like top: CPU seconds used over wall seconds elapsed,
 * × 100 (above 100% for multi-threaded processes). 0 without a usable previous sample,
 * or when the pid was reused (different command or CPU time going backwards).
 */
fun cpuPercents(previous: ProcessSample?, current: ProcessSample): Map<Int, Double> {
    val wallSeconds = previous?.let { (current.at - it.at) / 1000.0 } ?: 0.0
    val before = previous?.processes?.associateBy { it.pid }.orEmpty()
    return current.processes.associate { p ->
        val old = before[p.pid]
        val percent = when {
            wallSeconds <= 0 || old == null -> 0.0
            old.command != p.command || p.cpuTime < old.cpuTime -> 0.0
            else -> (p.cpuTime - old.cpuTime) / wallSeconds * 100
        }
        p.pid to percent
    }
}

fun processRows(previous: ProcessSample?, current: ProcessSample): List<ProcessRow> {
    val cpu = cpuPercents(previous, current)
    return current.processes.map { ProcessRow(it, cpu[it.pid] ?: 0.0) }
}

/** Rows whose command or arguments contain [filter] (case-insensitive), sorted by [sort]. */
fun List<ProcessRow>.filterAndSort(filter: String, sort: ProcessSort): List<ProcessRow> {
    val query = filter.trim()
    val matching = if (query.isEmpty()) this else filter {
        it.info.command.contains(query, ignoreCase = true) || it.info.args.contains(query, ignoreCase = true)
    }
    return when (sort) {
        ProcessSort.CPU -> matching.sortedWith(compareByDescending<ProcessRow> { it.cpuPercent }.thenByDescending { it.info.rss })
        ProcessSort.MEMORY -> matching.sortedWith(compareByDescending<ProcessRow> { it.info.rss }.thenByDescending { it.cpuPercent })
    }
}
