package name.levis.talosmobile.ui.theme

import android.os.Build
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.ColorScheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.dynamicDarkColorScheme
import androidx.compose.material3.dynamicLightColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import name.levis.talosmobile.data.ThemeMode

private val TalosOrange = Color(0xFFFF7A45)

private val DarkColors = darkColorScheme(
    primary = TalosOrange,
    onPrimary = Color(0xFF2B1000),
    background = Color(0xFF101418),
    surface = Color(0xFF101418),
    surfaceContainer = Color(0xFF1A1F24),
)

private val LightColors = lightColorScheme(
    primary = Color(0xFFB8461B),
)

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
data class ChartColors(val first: Color, val second: Color, val grid: Color)

private val LightChart = ChartColors(first = Color(0xFF2A78D6), second = Color(0xFFEB6834), grid = Color(0x1F000000))
private val DarkChart = ChartColors(first = Color(0xFF3987E5), second = Color(0xFFD95926), grid = Color(0x29FFFFFF))

val LocalChartColors = staticCompositionLocalOf { DarkChart }

@Composable
fun TalosTheme(mode: ThemeMode = ThemeMode.AUTO, content: @Composable () -> Unit) {
    val context = LocalContext.current
    val dark = mode.isDark(isSystemInDarkTheme())
    val dynamic = Build.VERSION.SDK_INT >= Build.VERSION_CODES.S
    val base = when {
        dynamic && dark -> dynamicDarkColorScheme(context).copy(primary = TalosOrange)
        dynamic -> dynamicLightColorScheme(context)
        dark -> DarkColors
        else -> LightColors
    }
    val colors = if (mode == ThemeMode.BLACK) base.trueBlack() else base

    CompositionLocalProvider(
        LocalStatusColors provides if (dark) DarkStatus else LightStatus,
        LocalChartColors provides if (dark) DarkChart else LightChart,
    ) {
        MaterialTheme(colorScheme = colors, content = content)
    }
}
