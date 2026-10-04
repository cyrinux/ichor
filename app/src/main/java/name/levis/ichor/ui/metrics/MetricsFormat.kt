package name.levis.ichor.ui.metrics

import name.levis.ichor.util.formatBytes
import java.util.Locale
import kotlin.math.abs

/** The units a panel can format its values in (Go's preset units). */
val METRICS_UNITS = listOf("", "percent", "bytes", "cores", "persec", "count")

/** [value] in [unit]: "42.1%", "1.5 GiB", "0.25 cores", "3.2/s", "7", or a compact plain number. */
fun formatMetric(value: Double, unit: String): String = when (unit) {
    "percent" -> String.format(Locale.ROOT, "%.1f%%", value)
    "bytes" -> if (value < 0) "-" + formatBytes(-value.toLong()) else formatBytes(value.toLong())
    "cores" -> String.format(Locale.ROOT, "%.2f", value)
    "persec" -> compact(value) + "/s"
    "count" -> if (abs(value) < 1e6) String.format(Locale.ROOT, "%.0f", value) else compact(value)
    else -> compact(value)
}

/** 1234567 -> "1.23M", 0.000123 -> "1.23e-4", 12.5 -> "12.5". */
internal fun compact(value: Double): String {
    val a = abs(value)
    return when {
        a == 0.0 -> "0"
        a >= 1e12 -> String.format(Locale.ROOT, "%.3g", value)
        a >= 1e9 -> String.format(Locale.ROOT, "%.2fG", value / 1e9)
        a >= 1e6 -> String.format(Locale.ROOT, "%.2fM", value / 1e6)
        a >= 1e4 -> String.format(Locale.ROOT, "%.1fk", value / 1e3)
        a >= 100 -> String.format(Locale.ROOT, "%.0f", value)
        // %.3g below 100 stays positional ("12.5", "1.00", "100"): trim decimals only.
        a >= 0.01 -> String.format(Locale.ROOT, "%.3g", value).let { if ('.' in it) it.trimEnd('0').trimEnd('.') else it }
        else -> String.format(Locale.ROOT, "%.2e", value)
    }
}
