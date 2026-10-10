package name.levis.ichor.ui.history

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.FilterChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.produceState
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.StrokeJoin
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.activeSummary
import name.levis.ichor.model.HISTORY_NOT_READY
import name.levis.ichor.model.HISTORY_READY
import name.levis.ichor.model.HistoryNodeSpan
import name.levis.ichor.model.HistoryQuery
import name.levis.ichor.model.uptimeSegments
import name.levis.ichor.ui.theme.LocalStatusColors
import java.util.Locale

/** The windows the history views offer, in days. */
const val HISTORY_SHORT_DAYS = 7
const val HISTORY_LONG_DAYS = 30
private const val DAY_MILLIS = 86_400_000L

/**
 * The shown cluster's history over the last [days] days, read once per cluster and window;
 * null while it loads, or when the cluster has none yet.
 */
@Composable
fun rememberClusterHistory(days: Int): HistoryQuery? {
    val app = LocalContext.current.applicationContext as TalosApp
    val config by app.configRepository.config.collectAsStateWithLifecycle()
    val fingerprint = config?.activeSummary?.fingerprint
    val history by produceState<HistoryQuery?>(null, fingerprint, days) {
        val now = System.currentTimeMillis()
        value = fingerprint?.let { app.historyRepository.query(it, now - days * DAY_MILLIS, now) }
    }
    return history
}

/**
 * A node's states over the window, as a strip: ready, not ready, unreachable, and no data
 * (a gap) left as the track. Its uptime % next to it, and the window to switch (7 or 30 days).
 */
@Composable
fun UptimeRow(span: HistoryNodeSpan, history: HistoryQuery, days: Int, onDays: (Int) -> Unit, modifier: Modifier = Modifier) {
    val percent = span.uptimePercent?.let { String.format(Locale.ROOT, "%.2f %%", it) } ?: "—"
    val description = pluralStringResource(R.plurals.history_uptime_description, days, days, percent)
    Row(modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        UptimeStrip(span, history.from, history.to, Modifier.weight(1f).semantics { contentDescription = description })
        Text(percent, style = MaterialTheme.typography.labelMedium)
        listOf(HISTORY_SHORT_DAYS, HISTORY_LONG_DAYS).forEach { window ->
            FilterChip(
                selected = window == days,
                onClick = { onDays(window) },
                label = { Text(pluralStringResource(R.plurals.history_days_short, window, window)) },
            )
        }
    }
}

/** The strip alone: one segment per state interval of [span] between [from] and [to]. */
@Composable
fun UptimeStrip(span: HistoryNodeSpan, from: Long, to: Long, modifier: Modifier = Modifier) {
    val colors = LocalStatusColors.current
    val track = MaterialTheme.colorScheme.surfaceVariant
    val segments = uptimeSegments(span, from, to)
    Canvas(modifier.height(12.dp)) {
        drawRect(track, size = size)
        segments.forEach { segment ->
            val color = when (segment.state) {
                HISTORY_READY -> colors.ok
                HISTORY_NOT_READY -> colors.warn
                else -> colors.bad
            }
            val left = size.width * segment.start
            // At least a hair, so a short outage over 30 days still shows.
            val width = maxOf(size.width * (segment.end - segment.start), 1.dp.toPx())
            drawRect(color, topLeft = Offset(left, 0f), size = Size(width, size.height))
        }
    }
}

/**
 * A percentage series (points [time, percent]) as a small line on a fixed 0–100 % scale over
 * [from]..[to], coloured by its last value like the usage bars.
 */
@Composable
fun PercentSparkline(series: List<List<Double>>, from: Long, to: Long, description: String, modifier: Modifier = Modifier) {
    val colors = LocalStatusColors.current
    val track = MaterialTheme.colorScheme.surfaceVariant
    val points = series.filter { it.size >= 2 }
    val last = points.lastOrNull()?.get(1) ?: 0.0
    val color = when {
        last >= 90 -> colors.bad
        last >= 75 -> colors.warn
        else -> colors.ok
    }
    Canvas(modifier.fillMaxWidth().height(20.dp).padding(vertical = 1.dp).semantics { contentDescription = description }) {
        val stroke = 1.5.dp.toPx()
        val span = (to - from).coerceAtLeast(1).toDouble()
        fun x(t: Double) = size.width * ((t - from) / span).toFloat().coerceIn(0f, 1f)
        fun y(v: Double) = stroke / 2 + (size.height - stroke) * (1f - (v / 100).toFloat().coerceIn(0f, 1f))

        drawLine(track, Offset(0f, size.height - stroke / 2), Offset(size.width, size.height - stroke / 2), strokeWidth = stroke)
        if (points.size < 2) return@Canvas
        val line = Path().apply {
            points.forEachIndexed { i, p -> if (i == 0) moveTo(x(p[0]), y(p[1])) else lineTo(x(p[0]), y(p[1])) }
        }
        drawPath(line, color, style = Stroke(stroke, cap = StrokeCap.Round, join = StrokeJoin.Round))
    }
}
