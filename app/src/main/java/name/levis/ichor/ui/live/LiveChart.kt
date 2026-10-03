package name.levis.ichor.ui.live

import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import name.levis.ichor.R
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.gestures.detectDragGestures
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.Card
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
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import kotlin.math.max

/** One line of a chart. Text never uses the series color; the legend dot carries identity. */
data class Series(val label: String, val color: Color, val values: List<Float>)

/**
 * Live line chart: one y-axis, 2px lines, recessive grid, current value(s) as the headline,
 * and drag/tap to scrub a crosshair that shows the values at that time.
 */
@Composable
fun LiveChart(
    title: String,
    series: List<Series>,
    times: List<Long>,
    format: (Float) -> String,
    gridColor: Color,
    modifier: Modifier = Modifier,
    fixedMax: Float? = null,
    maxPoints: Int = MAX_POINTS,
    pollSeconds: Long = POLL_SECONDS,
) {
    var selected by remember { mutableStateOf<Int?>(null) }
    val count = times.size
    val index = selected?.coerceIn(0, count - 1)
    val shownAt = index ?: (count - 1)

    Card(modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Row(verticalAlignment = Alignment.Bottom) {
                Text(title, style = MaterialTheme.typography.titleSmall, modifier = Modifier.weight(1f))
                if (index != null && count > 0) {
                    val seconds = ((times.last() - times[index]) / 1000).toInt()
                    Text(
                        if (seconds < 1) {
                            stringResource(R.string.node_live_now)
                        } else {
                            pluralStringResource(R.plurals.node_live_seconds_ago, seconds, seconds)
                        },
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
            // Headline: current (or scrubbed) value per series, in text ink.
            Row(horizontalArrangement = Arrangement.spacedBy(16.dp)) {
                series.forEach { s ->
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        if (series.size > 1) {
                            Box(Modifier.size(8.dp).background(s.color, CircleShape))
                            Spacer(Modifier.width(6.dp))
                            Text(s.label + " ", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        }
                        Text(
                            s.values.getOrNull(shownAt)?.let(format) ?: "—",
                            style = if (series.size > 1) MaterialTheme.typography.titleMedium else MaterialTheme.typography.headlineSmall,
                        )
                    }
                }
            }

            val chartDescription = stringResource(R.string.node_live_chart_description, title)
            val maxValue = fixedMax ?: max(series.maxOf { it.values.maxOrNull() ?: 0f } * 1.15f, 1e-3f)
            Box {
                Canvas(
                    Modifier
                        .fillMaxWidth()
                        .height(120.dp)
                        .semantics { contentDescription = chartDescription }
                        .pointerInput(count) {
                            detectTapGestures { offset -> selected = indexAt(offset.x, size.width.toFloat(), count, maxPoints) }
                        }
                        .pointerInput(count) {
                            detectDragGestures(
                                onDragEnd = { selected = null },
                                onDragCancel = { selected = null },
                            ) { change, _ -> selected = indexAt(change.position.x, size.width.toFloat(), count, maxPoints) }
                        },
                ) {
                    val w = size.width
                    val h = size.height
                    // Recessive grid: baseline, middle, top.
                    listOf(0f, 0.5f, 1f).forEach { f ->
                        drawLine(gridColor, Offset(0f, h * f), Offset(w, h * f), strokeWidth = 1.dp.toPx())
                    }
                    if (count < 2) return@Canvas
                    val step = w / (maxPoints - 1)
                    val startX = w - step * (count - 1)
                    series.forEach { s ->
                        val path = Path()
                        s.values.forEachIndexed { i, v ->
                            val x = startX + step * i
                            val y = h - (v / maxValue).coerceIn(0f, 1f) * h
                            if (i == 0) path.moveTo(x, y) else path.lineTo(x, y)
                        }
                        drawPath(path, s.color, style = Stroke(2.dp.toPx(), cap = StrokeCap.Round, join = StrokeJoin.Round))
                    }
                    index?.let { i ->
                        val x = startX + step * i
                        drawLine(gridColor.copy(alpha = 0.6f), Offset(x, 0f), Offset(x, h), strokeWidth = 1.dp.toPx())
                        series.forEach { s ->
                            val v = s.values.getOrNull(i) ?: return@forEach
                            val y = h - (v / maxValue).coerceIn(0f, 1f) * h
                            drawCircle(s.color, radius = 4.dp.toPx(), center = Offset(x, y))
                        }
                    }
                }
                Text(
                    format(maxValue),
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.align(Alignment.TopStart),
                )
            }
            Row {
                Text(stringResource(R.string.node_live_minutes_axis, (maxPoints * pollSeconds / 60).toInt()), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.weight(1f))
                Text(stringResource(R.string.node_live_now), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
    }
}

private fun indexAt(x: Float, width: Float, count: Int, maxPoints: Int): Int? {
    if (count < 2 || width <= 0f) return null
    val step = width / (maxPoints - 1)
    val startX = width - step * (count - 1)
    return ((x - startX) / step).toInt().coerceIn(0, count - 1)
}

