package name.levis.ichor.ui.theme

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.ColorScheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.remember
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.graphics.Color
import name.levis.ichor.data.ThemeMode
import name.levis.ichor.model.DEFAULT_CLUSTER_SEED

/** OLED "true black": pure black backgrounds, cards lifted just enough to stay distinct. */
private fun ColorScheme.trueBlack(): ColorScheme = copy(
    background = Color.Black,
    surface = Color.Black,
    surfaceDim = Color.Black,
    surfaceContainerLowest = Color.Black,
    surfaceContainerLow = Color(0xFF0B0B0B),
    surfaceContainer = Color(0xFF121212),
    surfaceContainerHigh = Color(0xFF181818),
    surfaceContainerHighest = Color(0xFF1F1F1F),
    surfaceBright = Color(0xFF242424),
)

/** Semantic status colours, tuned per theme for contrast. */
@Immutable
data class StatusColors(val ok: Color, val warn: Color, val bad: Color, val muted: Color)

private val DarkStatus = StatusColors(
    ok = Color(0xFF5BD18B),
    warn = Color(0xFFF2C14E),
    bad = Color(0xFFFF6B6B),
    muted = Color(0xFF8A939C),
)

private val LightStatus = StatusColors(
    ok = Color(0xFF1B7F45),
    warn = Color(0xFF8A6100),
    bad = Color(0xFFC0282D),
    muted = Color(0xFF5F6870),
)

val LocalStatusColors = staticCompositionLocalOf { DarkStatus }

/**
 * Chart series colors: categorical slots 1-2 (blue, orange) of the dataviz reference palette,
 * light and dark steps, validated (CVD and contrast) against the light, dark and true-black
 * surfaces. Single-series charts use [first].
 */
@Immutable
data class ChartColors(val first: Color, val second: Color, val third: Color, val grid: Color)

private val LightChart = ChartColors(first = Color(0xFF2A78D6), second = Color(0xFFEB6834), third = Color(0xFF1BAF7A), grid = Color(0x1F000000))
private val DarkChart = ChartColors(first = Color(0xFF3987E5), second = Color(0xFFD95926), third = Color(0xFF199E70), grid = Color(0x29FFFFFF))

val LocalChartColors = staticCompositionLocalOf { DarkChart }

/**
 * The app's theme: [mode] decides light, dark or true black; the colors are generated from
 * [seed], the main color of the cluster on screen.
 */
@Composable
fun TalosTheme(mode: ThemeMode = ThemeMode.AUTO, seed: Int = DEFAULT_CLUSTER_SEED, content: @Composable () -> Unit) {
    val dark = mode.isDark(isSystemInDarkTheme())
    val colors = remember(seed, dark, mode) {
        val base = seedColorScheme(seed, dark)
        if (mode == ThemeMode.BLACK) base.trueBlack() else base
    }

    CompositionLocalProvider(
        LocalStatusColors provides if (dark) DarkStatus else LightStatus,
        LocalChartColors provides if (dark) DarkChart else LightChart,
    ) {
        MaterialTheme(colorScheme = colors, content = content)
    }
}
