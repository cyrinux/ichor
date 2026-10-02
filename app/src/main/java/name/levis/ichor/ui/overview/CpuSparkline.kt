package name.levis.ichor.ui.overview

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.StrokeJoin
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * Recent cluster CPU as a small filled line on a fixed 0–100 % scale, newest on the right; it
 * fills in from the right as samples arrive. Coloured like [name.levis.ichor.ui.components.UsageBar].
 */
@Composable
fun CpuSparkline(history: List<Float>, modifier: Modifier = Modifier, warnAt: Float = 0.75f) {
    val colors = LocalStatusColors.current
    val current = history.lastOrNull() ?: 0f
    val color = when {
        current >= 0.9f -> colors.bad
        current >= warnAt -> colors.warn
        else -> colors.ok
    }
    val track = MaterialTheme.colorScheme.surfaceVariant
    val description = stringResource(R.string.overview_cpu_history_description)
    Canvas(modifier.fillMaxWidth().height(20.dp).semantics { contentDescription = description }) {
        val w = size.width
        val h = size.height
        val stroke = 1.5.dp.toPx()
        // Keep the line inside the canvas at 0 % and 100 %.
        fun y(v: Float) = stroke / 2 + (h - stroke) * (1f - v.coerceIn(0f, 1f))

        drawLine(track, Offset(0f, h - stroke / 2), Offset(w, h - stroke / 2), strokeWidth = stroke)
        if (history.size < 2) return@Canvas
        val step = w / (CLUSTER_HISTORY_POINTS - 1)
        val startX = w - step * (history.size - 1)
        val line = Path().apply {
            history.forEachIndexed { i, v -> if (i == 0) moveTo(startX, y(v)) else lineTo(startX + step * i, y(v)) }
        }
        val area = Path().apply {
            addPath(line)
            lineTo(w, h)
            lineTo(startX, h)
            close()
        }
        drawPath(area, Brush.verticalGradient(listOf(color.copy(alpha = 0.35f), color.copy(alpha = 0.04f))))
        drawPath(line, color, style = Stroke(stroke, cap = StrokeCap.Round, join = StrokeJoin.Round))
    }
}
