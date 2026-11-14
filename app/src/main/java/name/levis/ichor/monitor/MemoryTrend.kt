package name.levis.ichor.monitor

import name.levis.ichor.model.HistoryMemoryForecast
import name.levis.ichor.model.NodeMemoryForecast
import name.levis.ichor.model.forecastDays
import kotlin.math.floor

/** The volume name a memory [TrendDetail] carries: the wording names the node only. */
private const val MEMORY_TREND_NAME = "memory"

/** The trend detail of [node]'s memory, as [trendDetailOf] makes one for a volume. */
fun memoryTrendDetailOf(node: NodeMemoryForecast): TrendDetail {
    val full = node.daysToFull
    val critical = node.daysToCritical
    val sayFull = full != null && (critical == null || critical >= full)
    val days = if (sayFull) full else critical
    return TrendDetail(
        hostname = node.hostname.ifBlank { node.node },
        name = MEMORY_TREND_NAME,
        critical = !sayFull,
        days = days?.let(::forecastDays) ?: 0,
        slopePerDay = node.slopePerDay,
        percent = floor(node.usedPercent).toInt(),
    )
}

/**
 * One check of the nodes' memory trends of [forecast] against the ones [open] before, with the
 * storage trend's hysteresis: projected critical within [TREND_OPEN_DAYS] (a confident fit) opens
 * at once; an open one stays while within [TREND_CLOSE_DAYS], and resolves past it, without a
 * projection or without a row (a node gone or rebooted: its series restarted).
 */
fun memoryTrendStep(open: Map<String, String>, forecast: HistoryMemoryForecast): TrendStep {
    val nodes = forecast.nodes.associateBy { it.node }
    val next = sortedMapOf<String, String>()
    val alerts = mutableListOf<Alert>()
    (open.keys + nodes.keys).toSortedSet().forEach { node ->
        val before = open[node]
        val row = nodes[node]
        val days = row?.takeIf { it.confident }?.daysToCritical
        val detail = row?.let { memoryTrendDetailOf(it).format() }
        when {
            detail != null && days != null && days <= TREND_OPEN_DAYS -> {
                next[node] = detail
                if (before == null) alerts += memoryTrendAlert(node, detail, problem = true)
            }
            before != null && detail != null && days != null && days <= TREND_CLOSE_DAYS -> next[node] = detail
            before != null -> alerts += memoryTrendAlert(node, before, problem = false)
        }
    }
    return TrendStep(next, alerts)
}

/** [node] the address, [value] a [TrendDetail]: the node's hostname is the subject. */
private fun memoryTrendAlert(node: String, value: String, problem: Boolean): Alert = Alert(
    key = "memory:$node:trend",
    kind = if (problem) AlertKind.MEMORY_TREND else AlertKind.MEMORY_TREND_OK,
    problem = problem,
    subject = TrendDetail.parse(value).hostname,
    detail = value,
)

/**
 * [run] with each cluster's memory trends stepped (see [memoryTrendStep]) from its [before]
 * snapshot, by [forecasts] (monitor key → the forecast read after this run's record). A cluster
 * that could not be read, or without a forecast, keeps its open trends; with the alerts
 * ([enabled]) off, they are forgotten.
 */
fun withMemoryTrends(
    run: MonitorRun,
    before: MonitorState,
    reads: List<ClusterRead>,
    forecasts: Map<String, HistoryMemoryForecast>,
    enabled: Boolean,
): MonitorRun {
    val clusters = run.state.clusters.toMutableMap()
    val found = mutableMapOf<String, List<Alert>>()
    reads.filterNot { it.skipped }.forEach { read ->
        val snapshot = read.snapshot ?: return@forEach
        val key = monitorKeyOf(read.context)
        val kept = clusters[key] ?: return@forEach
        val known = before.clusters[key]?.takeIf { it.context == snapshot.context }?.memoryTrends.orEmpty()
        val forecast = forecasts[key]
        val trends = when {
            !enabled -> emptyMap()
            snapshot.unreachableAsAWhole || forecast == null -> known
            else -> {
                val step = memoryTrendStep(known, forecast)
                if (step.alerts.isNotEmpty()) found[key] = step.alerts
                step.open
            }
        }
        clusters[key] = kept.copy(memoryTrends = trends)
    }
    val merged = run.alerts.map { cluster ->
        found.remove(monitorKeyOf(cluster.context))?.let { cluster.copy(alerts = cluster.alerts + it) } ?: cluster
    }
    val added = reads.mapNotNull { read -> found[monitorKeyOf(read.context)]?.let { ClusterAlerts(read.context, it) } }
    return MonitorRun(run.state.copy(clusters = clusters), merged + added)
}

/** Whether [read] may have a memory forecast worth stepping: read this time, some node answering. */
fun wantsMemoryForecast(read: ClusterRead): Boolean {
    val snapshot = read.snapshot ?: return false
    return !read.skipped && !snapshot.unreachableAsAWhole
}
