package name.levis.ichor.util

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

/**
 * Compact duration patterns (positional %1$d/%2$d), so the UI can pass translated ones
 * (see ui/components/Durations.kt); [ENGLISH] is the default.
 */
data class DurationFormat(
    val daysHours: String,
    val hoursMinutes: String,
    val minutes: String,
    val lessThanMinute: String,
) {
    companion object {
        val ENGLISH = DurationFormat("%1\$dd %2\$dh", "%1\$dh %2\$dm", "%1\$dm", "<1m")
    }
}

/** Seconds -> "3d 4h", "5h 12m", "42m" or "<1m" (in English). */
fun formatDuration(seconds: Long, format: DurationFormat = DurationFormat.ENGLISH): String {
    if (seconds < 60) return format.lessThanMinute
    val days = seconds / 86_400
    val hours = (seconds % 86_400) / 3_600
    val minutes = (seconds % 3_600) / 60
    return when {
        days > 0 -> String.format(Locale.ROOT, format.daysHours, days, hours)
        hours > 0 -> String.format(Locale.ROOT, format.hoursMinutes, hours, minutes)
        else -> String.format(Locale.ROOT, format.minutes, minutes)
    }
}

/** Fraction used in [0, 1]; 0 when total is unknown. */
fun usedFraction(total: Long, available: Long): Float =
    if (total <= 0) 0f else ((total - available).coerceIn(0, total).toFloat() / total)

/** Days until an epoch-seconds deadline, negative when past. */
fun daysUntil(epochSeconds: Long, nowMillis: Long = System.currentTimeMillis()): Long =
    Math.floorDiv(epochSeconds * 1000 - nowMillis, 86_400_000L)
