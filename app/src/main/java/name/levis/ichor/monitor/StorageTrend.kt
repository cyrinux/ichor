package name.levis.ichor.monitor

import name.levis.ichor.model.HistoryForecast
import name.levis.ichor.model.VolumeForecast
import name.levis.ichor.model.forecastDays
import java.util.Locale
import kotlin.math.floor

/** A volume projected critical within this many days opens its trend alert… */
const val TREND_OPEN_DAYS = 3.0

/** …which stays open while the projection stays within this many (hysteresis), and resolves past it. */
const val TREND_CLOSE_DAYS = 7.0

private const val WORDING_CRITICAL = "critical"
private const val WORDING_FULL = "full"

/**
 * A volume trend alert's stored value, split: "hostname|volume|critical or full|days|slope|percent".
 * [critical]: the title says "critical in" (it comes first), else "full in"; [days] whole days,
 * 0 for less than one; [slopePerDay] percentage points a day; [percent] used now (floored).
 */
data class TrendDetail(
    val hostname: String,
    val name: String,
    val critical: Boolean,
    val days: Int,
    val slopePerDay: Double,
    val percent: Int,
) {
    fun format(): String = listOf(
        hostname.unpiped(), name.unpiped(), if (critical) WORDING_CRITICAL else WORDING_FULL,
        days.toString(), String.format(Locale.ROOT, "%.2f", slopePerDay), percent.toString(),
    ).joinToString("|")

    companion object {
        fun parse(value: String): TrendDetail {
            val parts = value.split('|', limit = 6)
            fun part(i: Int) = parts.getOrElse(i) { "" }
            return TrendDetail(
                hostname = part(0),
                name = part(1),
                critical = part(2) == WORDING_CRITICAL,
                days = part(3).toIntOrNull() ?: 0,
                slopePerDay = part(4).toDoubleOrNull() ?: 0.0,
                percent = part(5).toIntOrNull() ?: 0,
            )
        }
    }
}

/**
 * The trend detail of [volume] on the node named [hostname]: "critical in" when critical comes
 * before full (or full is beyond a year), else "full in".
 */
fun trendDetailOf(volume: VolumeForecast, hostname: String): TrendDetail {
    val full = volume.daysToFull
    val critical = volume.daysToCritical
    val sayFull = full != null && (critical == null || critical >= full)
    val days = if (sayFull) full else critical
    return TrendDetail(
        hostname = hostname,
        name = volume.name.ifBlank { volume.key.substringAfter('|') },
        critical = !sayFull,
        days = days?.let(::forecastDays) ?: 0,
        slopePerDay = volume.slopePerDay,
        percent = floor(volume.usedPercent).toInt(),
    )
}

/** The volumes' trend alerts open after a step ("node|volume" → [TrendDetail]), and what it notifies. */
data class TrendStep(val open: Map<String, String>, val alerts: List<Alert>)

/**
 * One check of the volume trends of [forecast] against the ones [open] before: a volume projected
 * critical within [TREND_OPEN_DAYS] (a confident fit) opens at once (it is already a week's trend);
 * an open one stays while within [TREND_CLOSE_DAYS], and resolves once past it, without a
 * projection or without a row. A volume in [fillIssues] (at or above the warning threshold) is
 * left to its fill alert: never opened, dropped quietly. [hostnames]: node address → name.
 */
fun storageTrendStep(
    open: Map<String, String>,
    forecast: HistoryForecast,
    fillIssues: Set<String>,
    hostnames: Map<String, String>,
): TrendStep {
    val volumes = forecast.volumes.associateBy { it.key }
    val next = sortedMapOf<String, String>()
    val alerts = mutableListOf<Alert>()
    (open.keys + volumes.keys).toSortedSet().forEach { key ->
        if (key in fillIssues) return@forEach
        val before = open[key]
        val volume = volumes[key]
        val days = volume?.takeIf { it.confident }?.daysToCritical
        val detail = volume?.let { trendDetailOf(it, hostnames[it.node].orEmpty().ifBlank { it.node }).format() }
        when {
            detail != null && days != null && days <= TREND_OPEN_DAYS -> {
                next[key] = detail
                if (before == null) alerts += trendAlert(key, detail, problem = true)
            }
            before != null && detail != null && days != null && days <= TREND_CLOSE_DAYS -> next[key] = detail
            before != null -> alerts += trendAlert(key, before, problem = false)
        }
    }
    return TrendStep(next, alerts)
}

/** [key] "node|volume", [value] a [TrendDetail]: the node's hostname is the subject. */
private fun trendAlert(key: String, value: String, problem: Boolean): Alert = Alert(
    key = "storage:$key:trend",
    kind = if (problem) AlertKind.STORAGE_TREND else AlertKind.STORAGE_TREND_OK,
    problem = problem,
    subject = TrendDetail.parse(value).hostname,
    detail = value,
)

/**
 * [run] with each cluster's volume trends stepped (see [storageTrendStep]) from its [before]
 * snapshot, by [forecasts] (monitor key → the forecast read after this run's record). A cluster
 * whose storage could not be read, or without a forecast, keeps its open trends; with the trend
 * alerts ([enabled]) or the storage watch off, they are forgotten.
 */
fun withStorageTrends(
    run: MonitorRun,
    before: MonitorState,
    reads: List<ClusterRead>,
    forecasts: Map<String, HistoryForecast>,
    enabled: Boolean,
): MonitorRun {
    val clusters = run.state.clusters.toMutableMap()
    val found = mutableMapOf<String, List<Alert>>()
    reads.filterNot { it.skipped }.forEach { read ->
        val snapshot = read.snapshot ?: return@forEach
        val key = monitorKeyOf(read.context)
        val kept = clusters[key] ?: return@forEach
        val known = before.clusters[key]?.takeIf { it.context == snapshot.context }?.storageTrends.orEmpty()
        val forecast = forecasts[key]
        val trends = when {
            !enabled || !snapshot.storageWatched -> emptyMap()
            snapshot.unreachableAsAWhole || !snapshot.storageChecked || forecast == null -> known
            else -> {
                val hostnames = snapshot.nodes.mapValues { it.value.hostname }
                val step = storageTrendStep(known, forecast, snapshot.storageIssues.keys, hostnames)
                if (step.alerts.isNotEmpty()) found[key] = step.alerts
                step.open
            }
        }
        clusters[key] = kept.copy(storageTrends = trends)
    }
    val merged = run.alerts.map { cluster ->
        found.remove(monitorKeyOf(cluster.context))?.let { cluster.copy(alerts = cluster.alerts + it) } ?: cluster
    }
    val added = reads.mapNotNull { read -> found[monitorKeyOf(read.context)]?.let { ClusterAlerts(read.context, it) } }
    return MonitorRun(run.state.copy(clusters = clusters), merged + added)
}

/** Whether [read] may have a forecast worth stepping: its storage watched and read this time. */
fun wantsStorageForecast(read: ClusterRead): Boolean {
    val snapshot = read.snapshot ?: return false
    return !read.skipped && snapshot.storageWatched && snapshot.storageChecked && !snapshot.unreachableAsAWhole
}

/** Without the separator, so a value always splits back the same way. */
private fun String.unpiped(): String = replace('|', '/')
