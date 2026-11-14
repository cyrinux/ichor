package name.levis.ichor.model

import kotlinx.serialization.Serializable

/*
 * Each node's memory trend over the last 7 days of the history ring (Ichorgo.historyMemoryForecast):
 * the volume forecast's fit on the memory series, a leak detector.
 */

/** The memory use a node's projection counts to: where kubelet's memory pressure and evictions start. */
const val MEMORY_CRITICAL_PERCENT = 90

@Serializable
data class HistoryMemoryForecast(val nodes: List<NodeMemoryForecast> = emptyList())

/**
 * One node, [node] its address as the history keys it. [daysToFull] and [daysToCritical] count
 * from [latestAt], as [VolumeForecast]'s do.
 */
@Serializable
data class NodeMemoryForecast(
    val node: String,
    val hostname: String = "",
    val usedPercent: Double = 0.0,
    val latestAt: Long = 0,
    val slopePerDay: Double = 0.0,
    val daysToFull: Double? = null,
    val daysToCritical: Double? = null,
    val confidence: String = "",
    val points: Int = 0,
    val spanHours: Double = 0.0,
) {
    val confident: Boolean get() = confidence == FORECAST_HIGH
}

/** What [NodeMemoryForecast]'s line shows; null for a node with no projection whose memory does not grow. */
fun NodeMemoryForecast.outlook(): VolumeOutlook? = trendOutlook(confident, daysToFull, daysToCritical, slopePerDay)
