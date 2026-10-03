package name.levis.ichor.ui.workloads

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.gestures.detectHorizontalDragGestures
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
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
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.NETPERF_THROUGHPUT
import name.levis.ichor.model.NetPerfLatency
import name.levis.ichor.model.NetPerfReport
import name.levis.ichor.model.formatMbps
import name.levis.ichor.model.formatMicros
import name.levis.ichor.model.trendOf
import name.levis.ichor.ui.components.InfoHint
import name.levis.ichor.ui.theme.LocalChartColors
import java.text.DateFormat
import java.util.Date
import kotlin.math.max

/**
 * Pod-to-pod throughput and p50 latency of the saved tests from [client] to [server], oldest
 * on the left: one chart per measure (they do not share a scale), with a shared selection so a
 * tap shows the same test in both. Shown once the pair has two tests to compare.
 */
@Composable
internal fun NetPerfTrend(history: List<NetPerfReport>, client: String, server: String) {
    val trend = remember(history, client, server) { history.trendOf(client, server) }
    if (trend.size < 2) return
    var selected by remember(trend) { mutableStateOf<Int?>(null) }
    val shown = trend[selected ?: trend.lastIndex]
    val colors = LocalChartColors.current
    val muted = MaterialTheme.colorScheme.onSurfaceVariant

    Card(Modifier.fillMaxWidth().padding(horizontal = 16.dp)) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Column {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(stringResource(R.string.netperf_trend), style = MaterialTheme.typography.titleSmall)
                    InfoHint(stringResource(R.string.netperf_trend), stringResource(R.string.netperf_hint_trend))
                    Spacer(Modifier.weight(1f))
                    Text(
                        DateFormat.getDateTimeInstance(DateFormat.SHORT, DateFormat.SHORT).format(Date(shown.started)),
                        style = MaterialTheme.typography.labelSmall,
                        color = muted,
                    )
                }
                Text(
                    stringResource(R.string.netperf_trend_pair, client, server, trend.size),
                    style = MaterialTheme.typography.bodySmall,
                    fontFamily = FontFamily.Monospace,
                    color = muted,
                )
            }
            TrendChart(
                label = testLabel(NETPERF_THROUGHPUT),
                values = trend.map { it.throughputMbps },
                selected = selected,
                onSelect = { selected = it },
                format = ::formatMbps,
                color = colors.first,
                grid = colors.grid,
            )
            TrendChart(
                label = stringResource(R.string.netperf_trend_latency),
                values = trend.map { it.p50Us },
                selected = selected,
                onSelect = { selected = it },
                format = ::formatMicros,
                color = colors.first,
                grid = colors.grid,
            )
        }
    }
}

/**
 * One measure across the tests: evenly spaced points (tests are not evenly spaced in time),
 * a 2 dp line broken where a test has no value, and the selected (or latest) value as the
 * headline. A tap or a sideways drag picks a test (vertical drags still scroll the page).
 */
@Composable
private fun TrendChart(
    label: String,
    values: List<Double?>,
    selected: Int?,
    onSelect: (Int?) -> Unit,
    format: (Double) -> String,
    color: Color,
    grid: Color,
) {
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    val surface = CardDefaults.cardColors().containerColor
    val count = values.size
    val shownAt = selected ?: (count - 1)
    val maxValue = max((values.filterNotNull().maxOrNull() ?: 0.0) * 1.15, 1e-3)
    val description = stringResource(R.string.netperf_trend_description, label)

    Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Row(verticalAlignment = Alignment.Bottom) {
            Text(label, style = MaterialTheme.typography.bodyMedium, color = muted, modifier = Modifier.weight(1f))
            Text(values.getOrNull(shownAt)?.let(format) ?: "—", style = MaterialTheme.typography.titleMedium)
        }
        Box {
            Canvas(
                Modifier
                    .fillMaxWidth()
                    .height(72.dp)
                    .semantics { contentDescription = description }
                    .pointerInput(count) {
                        detectTapGestures { onSelect(trendIndexAt(it.x, size.width.toFloat(), INSET.toPx(), count)) }
                    }
                    .pointerInput(count) {
                        detectHorizontalDragGestures { change, _ -> onSelect(trendIndexAt(change.position.x, size.width.toFloat(), INSET.toPx(), count)) }
                    },
            ) {
                val inset = INSET.toPx()
                val w = size.width - inset * 2
                val h = size.height
                listOf(0f, 0.5f, 1f).forEach { f -> drawLine(grid, Offset(0f, h * f), Offset(size.width, h * f), strokeWidth = 1.dp.toPx()) }
                val step = w / (count - 1)
                fun at(i: Int, v: Double) = Offset(inset + step * i, h - (v / maxValue).toFloat().coerceIn(0f, 1f) * h)

                selected?.let { i -> drawLine(grid.copy(alpha = 0.6f), Offset(inset + step * i, 0f), Offset(inset + step * i, h), strokeWidth = 1.dp.toPx()) }
                values.zipWithNext().forEachIndexed { i, (a, b) ->
                    if (a != null && b != null) drawLine(color, at(i, a), at(i + 1, b), strokeWidth = 2.dp.toPx(), cap = StrokeCap.Round)
                }
                values.forEachIndexed { i, v ->
                    if (v == null) return@forEachIndexed
                    val r = if (i == shownAt) 5.dp.toPx() else 4.dp.toPx()
                    // A 2 dp surface ring keeps each point distinct from the line under it.
                    drawCircle(surface, radius = r + 2.dp.toPx(), center = at(i, v))
                    drawCircle(color, radius = r, center = at(i, v))
                }
            }
            Text(format(maxValue), style = MaterialTheme.typography.labelSmall, color = muted, modifier = Modifier.align(Alignment.TopStart))
        }
    }
}

private val INSET = 8.dp

private fun trendIndexAt(x: Float, width: Float, inset: Float, count: Int): Int? {
    if (count < 2 || width <= inset * 2) return null
    val step = (width - inset * 2) / (count - 1)
    return ((x - inset) / step + 0.5f).toInt().coerceIn(0, count - 1)
}

/**
 * Where the round trips fell, on a scale from the fastest (left) to the slowest (right): the
 * bar spans p50 to p99, the tick marks p90. A long bar, or one far to the right, is jitter.
 */
@Composable
internal fun NetPerfLatencyRange(latency: NetPerfLatency) {
    if (latency.max <= latency.min) return
    val colors = LocalChartColors.current
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    val surface = CardDefaults.cardColors().containerColor
    val description = stringResource(
        R.string.netperf_latency_range_description,
        formatMicros(latency.min), formatMicros(latency.p50), formatMicros(latency.p99), formatMicros(latency.max),
    )
    Column(Modifier.padding(top = 4.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Canvas(Modifier.fillMaxWidth().height(14.dp).semantics { contentDescription = description }) {
            val cap = 4.dp.toPx()
            val w = size.width - cap * 2
            val mid = size.height / 2
            fun x(us: Double) = cap + ((us - latency.min) / (latency.max - latency.min)).toFloat().coerceIn(0f, 1f) * w
            drawLine(colors.grid.copy(alpha = 0.9f), Offset(cap, mid), Offset(cap + w, mid), strokeWidth = 2.dp.toPx(), cap = StrokeCap.Round)
            drawLine(colors.first, Offset(x(latency.p50), mid), Offset(x(latency.p99), mid), strokeWidth = 8.dp.toPx(), cap = StrokeCap.Round)
            drawLine(surface, Offset(x(latency.p90), mid - cap), Offset(x(latency.p90), mid + cap), strokeWidth = 2.dp.toPx())
        }
        Row {
            Text(stringResource(R.string.netperf_latency_min, formatMicros(latency.min)), style = MaterialTheme.typography.labelSmall, color = muted, modifier = Modifier.weight(1f))
            Text(stringResource(R.string.netperf_latency_max, formatMicros(latency.max)), style = MaterialTheme.typography.labelSmall, color = muted)
        }
    }
}
