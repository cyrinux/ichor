package name.levis.talosmobile.model

import kotlinx.serialization.Serializable

/** One sample of cumulative counters from the Go core (NodeStats). */
@Serializable
data class NodeStats(
    val at: Long,
    val cpuBusy: Double,
    val cpuTotal: Double,
    val cpuCount: Int = 0,
    val memTotal: Long,
    val memAvailable: Long,
    val load1: Double,
    val netRx: Long,
    val netTx: Long,
    val diskRead: Long,
    val diskWrite: Long,
)

/** Rates between two samples, ready to plot. */
data class StatsPoint(
    val at: Long,
    val cpuPercent: Float,
    val memPercent: Float,
    val memUsed: Long,
    val load1: Float,
    val rxPerSec: Float,
    val txPerSec: Float,
    val readPerSec: Float,
    val writePerSec: Float,
)

/**
 * Turns two consecutive samples into rates. Counters that went backwards (node reboot,
 * interface reset) give 0 rather than a huge negative spike; null if no time elapsed.
 */
fun ratesBetween(prev: NodeStats, cur: NodeStats): StatsPoint? {
    val seconds = (cur.at - prev.at) / 1000f
    if (seconds <= 0f) return null

    fun perSec(a: Long, b: Long) = if (b >= a) (b - a) / seconds else 0f

    val cpuDelta = cur.cpuTotal - prev.cpuTotal
    val cpu = if (cpuDelta > 0) ((cur.cpuBusy - prev.cpuBusy) / cpuDelta * 100).toFloat().coerceIn(0f, 100f) else 0f
    val used = (cur.memTotal - cur.memAvailable).coerceAtLeast(0)
    val mem = if (cur.memTotal > 0) used * 100f / cur.memTotal else 0f

    return StatsPoint(
        at = cur.at,
        cpuPercent = cpu,
        memPercent = mem,
        memUsed = used,
        load1 = cur.load1.toFloat(),
        rxPerSec = perSec(prev.netRx, cur.netRx),
        txPerSec = perSec(prev.netTx, cur.netTx),
        readPerSec = perSec(prev.diskRead, cur.diskRead),
        writePerSec = perSec(prev.diskWrite, cur.diskWrite),
    )
}
