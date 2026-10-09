package name.levis.ichor.ui.logs

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.LogLevel
import name.levis.ichor.model.LogRow
import name.levis.ichor.model.isErrorKey
import name.levis.ichor.model.logLevel
import name.levis.ichor.ui.theme.LocalStatusColors
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.time.format.FormatStyle
import name.levis.ichor.ui.components.expandable
import name.levis.ichor.ui.components.MonoScreen
import name.levis.ichor.ui.components.ScalableMonoText
import name.levis.ichor.ui.components.monoTextStyle
import name.levis.ichor.ui.components.monoTextActions

private val StripeWidth = 3.dp
private const val DEBUG_ALPHA = 0.6f
private val TimeFormat: DateTimeFormatter = DateTimeFormatter.ofPattern("HH:mm:ss.SSS")

/** Colours of a log row, resolved once per theme. */
@Immutable
data class LogPalette(val text: Color, val dim: Color, val value: Color, val error: Color, val warn: Color, val info: Color)

@Composable
fun rememberLogPalette(): LogPalette {
    val scheme = MaterialTheme.colorScheme
    val status = LocalStatusColors.current
    return remember(scheme, status) {
        LogPalette(
            text = scheme.onSurface,
            dim = scheme.onSurfaceVariant.copy(alpha = 0.7f),
            value = scheme.tertiary,
            error = scheme.error,
            warn = status.warn,
            info = scheme.onSurfaceVariant,
        )
    }
}

private fun LogPalette.levelColor(level: LogLevel): Color? = when (level) {
    LogLevel.ERROR -> error
    LogLevel.WARN -> warn
    LogLevel.INFO, LogLevel.DEBUG -> info
    LogLevel.NONE -> null
}

private val LogLevel.tag
    get() = when (this) {
        LogLevel.ERROR -> "ERR"
        LogLevel.WARN -> "WRN"
        LogLevel.INFO -> "INF"
        LogLevel.DEBUG -> "DBG"
        LogLevel.NONE -> ""
    }

private fun formatTime(ts: Long, zone: ZoneId): String = TimeFormat.format(Instant.ofEpochMilli(ts).atZone(zone))

/** `time [×N] LVL source msg key=value…`, coloured with [p]. */
fun logRowText(row: LogRow.Entry, p: LogPalette, zone: ZoneId): AnnotatedString = buildAnnotatedString {
    val entry = row.entry
    val level = entry.logLevel
    if (entry.ts > 0) {
        withStyle(SpanStyle(color = p.dim)) {
            if (row.count > 1 && row.firstTs > 0 && row.firstTs != entry.ts) append(formatTime(row.firstTs, zone) + '–')
            append(formatTime(entry.ts, zone))
        }
        append(' ')
    }
    if (row.count > 1) {
        withStyle(SpanStyle(color = p.value, fontWeight = FontWeight.Bold)) { append("×${row.count}") }
        append(' ')
    }
    p.levelColor(level)?.let { color ->
        withStyle(SpanStyle(color = color, fontWeight = FontWeight.Bold)) { append(level.tag) }
        append(' ')
    }
    if (entry.source.isNotEmpty()) {
        withStyle(SpanStyle(color = p.dim)) { append(entry.source) }
        append(' ')
    }
    withStyle(SpanStyle(color = p.text)) { append(entry.msg) }
    for (field in entry.fields) {
        val isError = isErrorKey(field.k)
        append(' ')
        withStyle(SpanStyle(color = if (isError) p.error else p.dim)) { append(field.k + '=') }
        withStyle(SpanStyle(color = if (isError) p.error else p.value)) { append(field.v) }
    }
}

/** One entry: level stripe and coloured text; tapping shows the original, selectable line. */
@Composable
fun LogEntryRow(row: LogRow.Entry, palette: LogPalette, zone: ZoneId, expanded: Boolean, onToggle: () -> Unit) {
    val level = row.entry.logLevel
    val text = remember(row, palette, zone) { logRowText(row, palette, zone) }
    val stripe = palette.levelColor(level)
    Column(
        Modifier
            .fillMaxWidth()
            .expandable(expanded, onToggle = onToggle)
            .monoTextActions()
            .then(if (level == LogLevel.DEBUG) Modifier.alpha(DEBUG_ALPHA) else Modifier)
            .drawBehind { if (stripe != null) drawRect(stripe, size = Size(StripeWidth.toPx(), size.height)) }
            .padding(start = StripeWidth + 5.dp, top = 1.dp, bottom = 1.dp),
    ) {
        Text(text, style = monoTextStyle())
        if (expanded) {
            SelectionContainer {
                Column(
                    Modifier
                        .fillMaxWidth()
                        .padding(vertical = 4.dp)
                        .background(MaterialTheme.colorScheme.surfaceContainerHigh, RoundedCornerShape(4.dp))
                        .padding(6.dp),
                ) {
                    if (row.count > 1) {
                        Text(
                            stringResource(R.string.logs_repeated, row.count),
                            style = MaterialTheme.typography.labelSmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                    Text(row.entry.text, style = monoTextStyle())
                }
            }
        }
    }
}

/** The local date of the entries below it. */
@Composable
fun LogDayRow(row: LogRow.Day) {
    val label = remember(row.date) { DateTimeFormatter.ofLocalizedDate(FormatStyle.FULL).format(row.date) }
    Text(
        label,
        style = MaterialTheme.typography.labelMedium,
        color = MaterialTheme.colorScheme.primary,
        modifier = Modifier.fillMaxWidth().padding(top = 8.dp, bottom = 2.dp),
    )
}
