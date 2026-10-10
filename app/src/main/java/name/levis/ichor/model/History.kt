package name.levis.ichor.model

import kotlinx.serialization.Serializable

/*
 * The 30-day history ring the Go core keeps per cluster (Ichorgo.historyAppend and friends):
 * one [HistoryRecord] per monitor run in, [HistoryQuery] and [HistorySince] out. Times are
 * epoch milliseconds.
 */

/** Node health in a record, as the Go core spells it. */
const val HISTORY_READY = "ready"
const val HISTORY_NOT_READY = "notReady"
const val HISTORY_UNREACHABLE = "unreachable"

/** One monitor run of one cluster. [reachable] false: no node answered, a gap rather than downtime. */
@Serializable
data class HistoryRecord(
    val at: Long,
    val reachable: Boolean = true,
    val nodes: List<HistoryNodeEntry> = emptyList(),
    val volumes: List<HistoryVolumeEntry> = emptyList(),
    val alerts: List<HistoryAlertEntry> = emptyList(),
)

@Serializable
data class HistoryNodeEntry(
    val node: String,
    val hostname: String = "",
    val health: String,
    val version: String? = null,
    val memUsedPercent: Double? = null,
)

@Serializable
data class HistoryVolumeEntry(val key: String, val name: String, val node: String, val usedPercent: Double)

/** An issue open after the run; [track] etcd, data, gitops, checkup, am, storage or cert. */
@Serializable
data class HistoryAlertEntry(
    val key: String,
    val track: String,
    val severity: String? = null,
    val title: String? = null,
)

/** What the ring holds between two times (Ichorgo.historyQuery). */
@Serializable
data class HistoryQuery(
    val from: Long = 0,
    val to: Long = 0,
    val records: Int = 0,
    val resetAt: Long? = null,
    val resetReason: String? = null,
    val nodes: List<HistoryNodeSpan> = emptyList(),
    val alerts: List<HistoryAlertSpan> = emptyList(),
    val volumes: List<HistoryVolumeSeries> = emptyList(),
    val memory: List<HistoryMemorySeries> = emptyList(),
    val gaps: List<HistoryInterval> = emptyList(),
)

@Serializable
data class HistoryNodeSpan(
    val node: String,
    val hostname: String = "",
    /** Ready time over observed time, gaps left out; null when the node was never observed. */
    val uptimePercent: Double? = null,
    val intervals: List<HistoryState> = emptyList(),
)

/** A node's [state] (ready, notReady, unreachable) from [from] to [to]. */
@Serializable
data class HistoryState(val state: String, val from: Long, val to: Long)

@Serializable
data class HistoryInterval(val from: Long, val to: Long)

@Serializable
data class HistoryAlertSpan(
    val key: String,
    val track: String = "",
    val severity: String = "",
    val title: String = "",
    val openedAt: Long = 0,
    val closedAt: Long? = null,
)

/** Points are [time, percent]. */
@Serializable
data class HistoryVolumeSeries(val key: String, val name: String = "", val node: String = "", val series: List<List<Double>> = emptyList())

@Serializable
data class HistoryMemorySeries(val node: String, val series: List<List<Double>> = emptyList())

/** What happened after the user last looked (Ichorgo.historySince). */
@Serializable
data class HistorySince(
    val from: Long = 0,
    val to: Long = 0,
    val records: Int = 0,
    val nodesRecovered: List<HistoryNodeOutage> = emptyList(),
    val nodesDown: List<HistoryNodeOutage> = emptyList(),
    val alertsResolved: List<HistoryAlertSpan> = emptyList(),
    val alertsOpen: List<HistoryAlertSpan> = emptyList(),
    val upgrades: List<HistoryUpgrade> = emptyList(),
)

@Serializable
data class HistoryNodeOutage(
    val node: String,
    val hostname: String = "",
    val state: String = "",
    val downAt: Long = 0,
    val upAt: Long? = null,
)

@Serializable
data class HistoryUpgrade(val node: String, val hostname: String = "", val from: String = "", val to: String = "", val at: Long = 0)

/** The alerts opened after [HistorySince.from]: older ones were there when the user last looked. */
val HistorySince.newAlerts: List<HistoryAlertSpan> get() = alertsOpen.filter { it.openedAt > from }

/** The nodes that went down after [HistorySince.from]. */
val HistorySince.newlyDown: List<HistoryNodeOutage> get() = nodesDown.filter { it.downAt > from }

/**
 * Whether anything happened since the user last looked: an alert open (or a node down) all
 * along is no news, one opened (or gone down) since is.
 */
val HistorySince.hasNews: Boolean
    get() = nodesRecovered.isNotEmpty() || newlyDown.isNotEmpty() || alertsResolved.isNotEmpty() ||
        upgrades.isNotEmpty() || newAlerts.isNotEmpty()

/** A node's name as a history entry gives it: its hostname, else its address. */
fun historyNodeName(node: String, hostname: String): String = hostname.ifBlank { node }

/** A stretch of a node's [state] as fractions (0–1) of the window shown. */
data class UptimeSegment(val state: String, val start: Float, val end: Float)

/**
 * The intervals of [span] within [from]..[to] as fractions of that window, in order; the time
 * between them (gaps, before the first record) has none. Empty for an empty window.
 */
fun uptimeSegments(span: HistoryNodeSpan, from: Long, to: Long): List<UptimeSegment> {
    if (to <= from) return emptyList()
    val length = (to - from).toDouble()
    return span.intervals.mapNotNull { interval ->
        val start = interval.from.coerceIn(from, to)
        val end = interval.to.coerceIn(from, to)
        if (end <= start) return@mapNotNull null
        UptimeSegment(interval.state, ((start - from) / length).toFloat(), ((end - from) / length).toFloat())
    }
}
