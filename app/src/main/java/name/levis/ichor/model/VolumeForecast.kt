package name.levis.ichor.model

import kotlinx.serialization.Serializable
import java.util.Locale
import kotlin.math.roundToInt

/*
 * Each volume's fill trend over the last 7 days of the history ring (Ichorgo.historyVolumeForecast):
 * its growth per day and, when the fit is good, the days until critical and until full.
 */

/** A trend good enough to project ([VolumeForecast.daysToFull] and [VolumeForecast.daysToCritical] may come). */
const val FORECAST_HIGH = "high"

/** The slope (percentage points a day) below which a volume reads as stable. */
const val FORECAST_MIN_SLOPE = 0.05

@Serializable
data class HistoryForecast(val volumes: List<VolumeForecast> = emptyList())

/**
 * One volume, [key] "node|volume" as the storage fill alerts key it. [daysToFull] and
 * [daysToCritical] count from [latestAt]; each is absent beyond a year, without a good fit, and
 * [daysToCritical] once the volume is at or past critical.
 */
@Serializable
data class VolumeForecast(
    val key: String,
    val name: String = "",
    val node: String = "",
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

/**
 * What a volume's row says of its trend: [full] and [critical] in whole days (0 for less than
 * a day), [critical] only when it comes before full; or, without a projection, [growingPerDay]
 * (percentage points a day) when it grows at all.
 */
data class VolumeOutlook(val full: Int? = null, val critical: Int? = null, val growingPerDay: Double? = null)

/** Days as the wording shows them: rounded, 0 below one day. */
fun forecastDays(days: Double): Int = if (days < 1) 0 else days.roundToInt()

/** What [VolumeForecast]'s row shows; null for a volume with no projection that does not grow. */
fun VolumeForecast.outlook(): VolumeOutlook? {
    if (confident) {
        val full = daysToFull?.let(::forecastDays)
        val critical = daysToCritical?.let(::forecastDays)?.takeIf { full == null || it < full }
        if (full != null || critical != null) return VolumeOutlook(full = full, critical = critical)
    }
    return if (slopePerDay >= FORECAST_MIN_SLOPE) VolumeOutlook(growingPerDay = slopePerDay) else null
}

/** "2.4" (one decimal under 10, whole above): a growth in percentage points a day. */
fun formatSlope(perDay: Double): String =
    if (perDay >= 10) String.format(Locale.ROOT, "%.0f", perDay) else String.format(Locale.ROOT, "%.1f", perDay)
