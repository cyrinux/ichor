package name.levis.ichor.ui.logs

import androidx.compose.material3.LocalContentColor
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.takeOrElse
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextDecoration
import androidx.compose.ui.text.withStyle
import name.levis.ichor.model.LogEntry
import name.levis.ichor.model.LogSpan
import name.levis.ichor.ui.components.monoTextStyle
import name.levis.ichor.ui.theme.LocalStatusColors
import kotlin.math.max
import kotlin.math.min
import kotlin.math.pow

/** Below this contrast with the surface (WCAG ratio), a log colour is nudged lighter or darker. */
const val MIN_ANSI_CONTRAST = 3.0

private const val DIM_ALPHA = 0.6f
private const val BRIGHT_ON_DARK = 0.3f
private const val BRIGHT_ON_LIGHT = 0.25f
private const val NUDGE_STEPS = 10

/** The 16 ANSI colours (0–7 normal, 8–15 bright) mapped onto the theme, and the [surface] they are drawn on. */
@Immutable
data class AnsiPalette(val colors: List<Color>, val surface: Color)

/**
 * The ANSI colours from the theme's: red, green and yellow from the error and status colours,
 * blue, magenta and cyan from the scheme's accents. The colour the surface would swallow (black
 * on a dark theme, white on a light one) becomes the muted text colour; bright variants are
 * lighter on a dark surface and darker on a light one.
 */
fun ansiPalette(
    surface: Color,
    onSurface: Color,
    onSurfaceVariant: Color,
    red: Color,
    green: Color,
    yellow: Color,
    blue: Color,
    magenta: Color,
    cyan: Color,
): AnsiPalette {
    val dark = surface.isDark()
    val black = if (dark) onSurfaceVariant else onSurface
    val white = if (dark) onSurface else onSurfaceVariant
    val normal = listOf(black, red, green, yellow, blue, magenta, cyan, white)
    val bright = normal.map { if (dark) it.mix(Color.White, BRIGHT_ON_DARK) else it.mix(Color.Black, BRIGHT_ON_LIGHT) }
    return AnsiPalette(normal + bright, surface)
}

/** The palette of the current theme. */
@Composable
fun rememberAnsiPalette(): AnsiPalette {
    val scheme = MaterialTheme.colorScheme
    val status = LocalStatusColors.current
    return remember(scheme, status) {
        ansiPalette(
            surface = scheme.surface,
            onSurface = scheme.onSurface,
            onSurfaceVariant = scheme.onSurfaceVariant,
            red = scheme.error,
            green = status.ok,
            yellow = status.warn,
            blue = scheme.primary,
            magenta = scheme.tertiary,
            cyan = scheme.secondary,
        )
    }
}

/** "#rrggbb" as an opaque colour; null when malformed. */
fun parseHexColor(hex: String): Color? {
    if (hex.length != 7 || hex[0] != '#') return null
    val rgb = hex.substring(1).toLongOrNull(16) ?: return null
    return Color(0xFF000000 or rgb)
}

/** WCAG relative luminance (0 black – 1 white), alpha ignored. */
fun Color.relativeLuminance(): Double {
    fun channel(c: Float): Double = if (c <= 0.03928f) c / 12.92 else ((c + 0.055) / 1.055).pow(2.4)
    return 0.2126 * channel(red) + 0.7152 * channel(green) + 0.0722 * channel(blue)
}

/** WCAG contrast ratio between [a] and [b], from 1 to 21. */
fun contrastRatio(a: Color, b: Color): Double {
    val la = a.relativeLuminance()
    val lb = b.relativeLuminance()
    return (max(la, lb) + 0.05) / (min(la, lb) + 0.05)
}

private fun Color.isDark() = relativeLuminance() < 0.5

/** [this] moved [fraction] of the way toward [other] (alpha kept). */
private fun Color.mix(other: Color, fraction: Float) = Color(
    red = (red + (other.red - red) * fraction).coerceIn(0f, 1f),
    green = (green + (other.green - green) * fraction).coerceIn(0f, 1f),
    blue = (blue + (other.blue - blue) * fraction).coerceIn(0f, 1f),
    alpha = alpha,
)

/**
 * [this], or when its contrast with [surface] is under [MIN_ANSI_CONTRAST], the nearest step
 * toward white or black (whichever stands out more on [surface]) that reaches it: lightness
 * changes, the hue is kept as far as it can be.
 */
fun Color.readableOn(surface: Color): Color {
    if (contrastRatio(this, surface) >= MIN_ANSI_CONTRAST) return this
    val target = if (contrastRatio(Color.White, surface) >= contrastRatio(Color.Black, surface)) Color.White else Color.Black
    for (step in 1..NUDGE_STEPS) {
        val nudged = mix(target, step.toFloat() / NUDGE_STEPS)
        if (contrastRatio(nudged, surface) >= MIN_ANSI_CONTRAST) return nudged
    }
    return target
}

private fun AnsiPalette.color(index: Int, rgb: String): Color? = when {
    rgb.isNotEmpty() -> parseHexColor(rgb)
    index in colors.indices -> colors[index]
    else -> null
}

/** The style of [span] over text drawn in [base]; null for a span in the default style. */
fun LogSpan.style(palette: AnsiPalette, base: Color): SpanStyle? {
    val background = palette.color(bg, bgRgb)
    val foreground = palette.color(fg, fgRgb)
    if (background == null && foreground == null && !b && !d && !i && !u) return null
    val under = background ?: palette.surface
    val color = when {
        foreground != null -> foreground.readableOn(under)
        background != null -> base.readableOn(under)
        else -> null
    }
    return SpanStyle(
        color = if (d) (color ?: base).copy(alpha = DIM_ALPHA) else color ?: Color.Unspecified,
        background = background ?: Color.Unspecified,
        fontWeight = if (b) FontWeight.Bold else null,
        fontStyle = if (i) FontStyle.Italic else null,
        textDecoration = if (u) TextDecoration.Underline else null,
    )
}

/** The spans of a line as one styled string, the text of default spans in [base]. */
fun List<LogSpan>.toAnnotated(palette: AnsiPalette, base: Color): AnnotatedString = buildAnnotatedString {
    for (span in this@toAnnotated) {
        val style = span.style(palette, base)
        if (style == null) append(span.t) else withStyle(style) { append(span.t) }
    }
}

/**
 * One log line in its ANSI colours: [spans] drawn over the theme, or [text] as is when it had
 * none. One styled string per line: lists keep laying out only the visible ones.
 */
@Composable
fun AnsiLine(
    text: String,
    spans: List<LogSpan>,
    modifier: Modifier = Modifier,
    style: TextStyle = monoTextStyle(),
    color: Color = Color.Unspecified,
) {
    if (spans.isEmpty()) {
        Text(text, modifier, color = color, style = style)
        return
    }
    val palette = rememberAnsiPalette()
    val base = color.takeOrElse { style.color.takeOrElse { LocalContentColor.current } }
    val styled = remember(spans, palette, base) { spans.toAnnotated(palette, base) }
    Text(styled, modifier, color = color, style = style)
}

/** [entry]'s original line in its ANSI colours. */
@Composable
fun AnsiLine(entry: LogEntry, modifier: Modifier = Modifier, style: TextStyle = monoTextStyle(), color: Color = Color.Unspecified) =
    AnsiLine(entry.text, entry.spans, modifier, style, color)
