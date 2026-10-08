package name.levis.ichor.ui.dataservices

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.PathEffect
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import name.levis.ichor.ui.theme.LocalChartColors
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.formatBytes
import java.text.NumberFormat
import java.util.Currency
import java.util.Locale

/**
 * Before against after on one scale: the solid bar is the new value, the dashed outline the old
 * one, both as fractions of the larger. Reads the same way for requests and for costs.
 */
@Composable
fun BeforeAfterBar(before: Double, after: Double, modifier: Modifier = Modifier, height: Dp = 6.dp, scale: Double = maxOf(before, after)) {
    val fill = LocalChartColors.current.first
    val outline = MaterialTheme.colorScheme.onSurfaceVariant
    val track = MaterialTheme.colorScheme.surfaceContainerHighest
    Canvas(modifier.fillMaxWidth().height(height + 4.dp)) {
        if (scale <= 0) return@Canvas
        val h = height.toPx()
        val top = (size.height - h) / 2
        val radius = CornerRadius(h / 2, h / 2)
        drawRoundRect(track, Offset(0f, top), Size(size.width, h), radius)
        val afterWidth = (after / scale).toFloat().coerceIn(0f, 1f) * size.width
        if (afterWidth > 0f) drawRoundRect(fill, Offset(0f, top), Size(afterWidth.coerceAtLeast(h), h), radius)
        val beforeWidth = (before / scale).toFloat().coerceIn(0f, 1f) * size.width
        if (beforeWidth > 0f) {
            val inset = 1.dp.toPx()
            drawRoundRect(
                outline, Offset(inset, inset), Size((beforeWidth - 2 * inset).coerceAtLeast(h), size.height - 2 * inset),
                CornerRadius(size.height / 2, size.height / 2),
                style = Stroke(width = 1.dp.toPx(), pathEffect = PathEffect.dashPathEffect(floatArrayOf(4.dp.toPx(), 3.dp.toPx()))),
            )
        }
    }
}

/** "CPU  [bar]  500m → 120m": one resource's before and after on a row. */
@Composable
fun ResourceChangeRow(label: String, before: Double, after: Double, text: String) {
    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        Text(label, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.width(52.dp))
        BeforeAfterBar(before, after, Modifier.weight(1f))
        Text(text, style = MaterialTheme.typography.labelSmall, fontFamily = FontFamily.Monospace, modifier = Modifier.width(132.dp), maxLines = 1)
    }
}

/** A headline number with its caption above and unit below. */
@Composable
fun MetricTile(title: String, value: String, caption: String, color: Color, modifier: Modifier = Modifier) {
    Column(modifier, verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Text(title, style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
        Text(value, style = MaterialTheme.typography.headlineSmall, fontFamily = FontFamily.Monospace, fontWeight = FontWeight.Medium, color = color)
        Text(caption, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

/** One part of a [SplitBar]: its count, colour and legend label. */
data class SplitPart(val count: Int, val color: Color, val label: String)

/** A stacked bar of [parts] by count, with a legend under it; empty parts are left out. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun SplitBar(parts: List<SplitPart>, modifier: Modifier = Modifier) {
    val shown = parts.filter { it.count > 0 }
    if (shown.isEmpty()) return
    Column(modifier, verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Row(Modifier.fillMaxWidth().height(8.dp), horizontalArrangement = Arrangement.spacedBy(2.dp)) {
            shown.forEach { Box(Modifier.weight(it.count.toFloat()).height(8.dp).background(it.color, RoundedCornerShape(2.dp))) }
        }
        FlowRow(horizontalArrangement = Arrangement.spacedBy(14.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            shown.forEach { part ->
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Box(Modifier.size(8.dp).background(part.color, RoundedCornerShape(2.dp)))
                    Text(part.label, style = MaterialTheme.typography.labelMedium, modifier = Modifier.padding(start = 6.dp))
                }
            }
        }
    }
}

/** An amber banner: something to keep an eye on, not broken. */
@Composable
fun WarnBanner(text: String, modifier: Modifier = Modifier) {
    val warn = LocalStatusColors.current.warn
    Surface(color = warn.copy(alpha = 0.12f), contentColor = warn, shape = RoundedCornerShape(12.dp), modifier = modifier.fillMaxWidth()) {
        Text(text, style = MaterialTheme.typography.bodyMedium, modifier = Modifier.padding(12.dp))
    }
}

/** A saving is good news, growth a caution, no change neutral. */
@Composable
fun deltaColor(delta: Long): Color = when {
    delta < 0 -> LocalStatusColors.current.ok
    delta > 0 -> LocalStatusColors.current.warn
    else -> MaterialTheme.colorScheme.onSurfaceVariant
}

/** "+384.0 MiB", "-1.2 GiB", "0 B". */
fun signedBytes(bytes: Long): String = when {
    bytes < 0 -> "-" + formatBytes(-bytes)
    bytes > 0 -> "+" + formatBytes(bytes)
    else -> formatBytes(0)
}

/** [amount] of [currency] in the user's locale ("$418", "418 €"); the bare number for an unknown currency. */
fun formatMoney(amount: Double, currency: String, decimals: Int = 0): String = runCatching {
    NumberFormat.getCurrencyInstance(Locale.getDefault()).apply {
        this.currency = Currency.getInstance(currency)
        minimumFractionDigits = decimals
        maximumFractionDigits = decimals
    }.format(amount)
}.getOrElse { String.format(Locale.getDefault(), "%.${decimals}f", amount) }
