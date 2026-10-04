package name.levis.ichor.ui.metrics

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.gestures.detectHorizontalDragGestures
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.StrokeJoin
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.ui.theme.LocalChartColors
import name.levis.ichor.util.formatTime
import java.text.DateFormat
import kotlin.math.max
import kotlin.math.min

/** One line: [values] aligned on the chart's times, null for a gap. */
data class TimeLine(val label: String, val values: List<Double?>)

/**
 * A PromQL result over time: one y-axis from the lowest value (or 0) to the highest, 2 dp
 * lines broken at gaps. The first three lines get the chart colors in order (color follows
 * the series, not its rank); the others are muted, never a made-up hue. Below, every line
 * with its value at the scrubbed (or latest) time, so identity never rests on color alone.
 */
@Composable
fun TimeSeriesChart(
    title: String,
    times: List<Long>,
    lines: List<TimeLine>,
    format: (Double) -> String,
    modifier: Modifier = Modifier,
) {
    val chart = LocalChartColors.current
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    val palette = listOf(chart.first, chart.second, chart.third)
    fun colorOf(i: Int) = palette.getOrNull(i) ?: muted.copy(alpha = 0.45f)

    var selected by remember(times) { mutableStateOf<Int?>(null) }
    val count = times.size
    val shownAt = selected ?: lastFilled(lines, count)
    val all = lines.flatMap { it.values.filterNotNull() }
    val top = max(all.maxOrNull() ?: 0.0, 0.0)
    val bottom = min(all.minOrNull() ?: 0.0, 0.0)
    val span = max((top - bottom) * 1.1, 1e-9)
    val description = stringResource(R.string.metrics_chart_description, title)

    Column(modifier, verticalArrangement = Arrangement.spacedBy(6.dp)) {
        Box {
            Canvas(
                Modifier
                    .fillMaxWidth()
                    .height(140.dp)
                    .semantics { contentDescription = description }
                    .pointerInput(count) { detectTapGestures { selected = indexAt(it.x, size.width.toFloat(), count) } }
                    .pointerInput(count) {
                        detectHorizontalDragGestures(onDragEnd = { selected = null }, onDragCancel = { selected = null }) { change, _ ->
                            selected = indexAt(change.position.x, size.width.toFloat(), count)
                        }
                    },
            ) {
                val w = size.width
                val h = size.height
                listOf(0f, 0.5f, 1f).forEach { f -> drawLine(chart.grid, Offset(0f, h * f), Offset(w, h * f), strokeWidth = 1.dp.toPx()) }
                if (count == 0) return@Canvas
                val step = if (count > 1) w / (count - 1) else 0f
                fun at(i: Int, v: Double) = Offset(if (count > 1) step * i else w / 2, h - ((v - bottom) / span).toFloat().coerceIn(0f, 1f) * h)

                // Muted lines first, so the colored ones stay on top.
                lines.indices.reversed().forEach { li ->
                    val path = Path()
                    var drawing = false
                    lines[li].values.forEachIndexed { i, v ->
                        if (v == null) {
                            drawing = false
                        } else {
                            val p = at(i, v)
                            if (drawing) path.lineTo(p.x, p.y) else path.moveTo(p.x, p.y)
                            drawing = true
                        }
                    }
                    drawPath(path, colorOf(li), style = Stroke(2.dp.toPx(), cap = StrokeCap.Round, join = StrokeJoin.Round))
                    // A lone sample (instant query, or between gaps) still shows.
                    lines[li].values.forEachIndexed { i, v ->
                        val alone = v != null && lines[li].values.getOrNull(i - 1) == null && lines[li].values.getOrNull(i + 1) == null
                        if (alone) drawCircle(colorOf(li), radius = 3.dp.toPx(), center = at(i, v))
                    }
                }
                selected?.let { i ->
                    val x = if (count > 1) step * i else w / 2
                    drawLine(chart.grid.copy(alpha = 0.6f), Offset(x, 0f), Offset(x, h), strokeWidth = 1.dp.toPx())
                    lines.forEachIndexed { li, line ->
                        val v = line.values.getOrNull(i) ?: return@forEachIndexed
                        drawCircle(colorOf(li), radius = 4.dp.toPx(), center = at(i, v))
                    }
                }
            }
            Text(format(bottom + span), style = MaterialTheme.typography.labelSmall, color = muted, modifier = Modifier.align(Alignment.TopStart))
            if (bottom < 0) Text(format(bottom), style = MaterialTheme.typography.labelSmall, color = muted, modifier = Modifier.align(Alignment.BottomStart))
        }
        if (count > 0) {
            Row {
                Text(formatTime(times.first(), DateFormat.SHORT), style = MaterialTheme.typography.labelSmall, color = muted, modifier = Modifier.weight(1f))
                shownAt?.let { Text(formatTime(times[it], DateFormat.SHORT), style = MaterialTheme.typography.labelSmall, color = muted) }
            }
        }
        lines.forEachIndexed { li, line ->
            Row(verticalAlignment = Alignment.CenterVertically) {
                if (lines.size > 1) {
                    Box(Modifier.size(8.dp).background(colorOf(li), CircleShape))
                    Spacer(Modifier.width(8.dp))
                }
                Text(
                    line.label,
                    style = MaterialTheme.typography.bodySmall,
                    color = muted,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.weight(1f),
                )
                Text(
                    shownAt?.let { line.values.getOrNull(it) }?.let(format) ?: "—",
                    style = if (lines.size > 1) MaterialTheme.typography.bodyMedium else MaterialTheme.typography.headlineSmall,
                )
            }
        }
    }
}

/** The last time any line has a value, null when all are gaps. */
private fun lastFilled(lines: List<TimeLine>, count: Int): Int? =
    (count - 1 downTo 0).firstOrNull { i -> lines.any { it.values.getOrNull(i) != null } }

private fun indexAt(x: Float, width: Float, count: Int): Int? {
    if (count < 2 || width <= 0f) return if (count == 1) 0 else null
    return ((x / (width / (count - 1))) + 0.5f).toInt().coerceIn(0, count - 1)
}
