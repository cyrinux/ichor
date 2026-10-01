package name.levis.talosmobile.util

import java.util.Locale

private val UNITS = listOf("B", "KiB", "MiB", "GiB", "TiB", "PiB")

/** 1536 -> "1.5 KiB". */
fun formatBytes(bytes: Long): String {
    if (bytes < 1024) return "$bytes B"
    var value = bytes.toDouble()
    var unit = 0
    while (value >= 1024 && unit < UNITS.lastIndex) {
        value /= 1024
        unit++
    }
    return String.format(Locale.ROOT, "%.1f %s", value, UNITS[unit])
}

/** Seconds -> "3d 4h", "5h 12m", "42m" or "<1m". */
fun formatDuration(seconds: Long): String {
    if (seconds < 60) return "<1m"
    val days = seconds / 86_400
    val hours = (seconds % 86_400) / 3_600
    val minutes = (seconds % 3_600) / 60
    return when {
        days > 0 -> "${days}d ${hours}h"
        hours > 0 -> "${hours}h ${minutes}m"
        else -> "${minutes}m"
    }
}

/** Fraction used in [0, 1]; 0 when total is unknown. */
fun usedFraction(total: Long, available: Long): Float =
    if (total <= 0) 0f else ((total - available).coerceIn(0, total).toFloat() / total)

/** Days until an epoch-seconds deadline, negative when past. */
fun daysUntil(epochSeconds: Long, nowMillis: Long = System.currentTimeMillis()): Long =
    Math.floorDiv(epochSeconds * 1000 - nowMillis, 86_400_000L)
