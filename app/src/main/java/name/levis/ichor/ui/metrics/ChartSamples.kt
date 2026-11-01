package name.levis.ichor.ui.metrics

/**
 * The sample [delta] steps from [current], or from [latest] when none is selected yet (what the
 * chart shows then), or from the end when every line is a gap. Stays on the chart's times; null
 * without any.
 */
internal fun stepSample(current: Int?, latest: Int?, count: Int, delta: Int): Int? {
    if (count == 0) return null
    return ((current ?: latest ?: (count - 1)) + delta).coerceIn(0, count - 1)
}

/** What a screen reader says for one sample: its time, then each line with its value or [noValue]. */
internal fun sampleAnnouncement(time: String, lines: List<TimeLine>, index: Int, format: (Double) -> String, noValue: String): String {
    val values = lines.joinToString(", ") { line ->
        "${line.label}: ${line.values.getOrNull(index)?.let(format) ?: noValue}"
    }
    return "$time. $values"
}
