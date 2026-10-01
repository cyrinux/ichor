package name.levis.talosmobile.ui.theme

import androidx.compose.material3.ColorScheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.ui.graphics.Color
import name.levis.talosmobile.model.hsl
import name.levis.talosmobile.model.tonal

/** One hue at a fixed saturation, read at Material tones (0 black .. 100 white). */
internal class TonalPalette(private val hue: Float, private val saturation: Float) {
    fun argb(tone: Int): Int = tonal(hue, saturation, tone)
    operator fun get(tone: Int): Color = Color(argb(tone))
}

/**
 * The palettes a Material 3 scheme is made of, all derived from one [seed] color (a
 * cluster's main color): the seed's hue for primary, quieter for secondary, a neighbouring
 * hue for tertiary, and barely tinted neutrals for surfaces and text.
 */
internal class SeedPalettes(seed: Int) {
    private val hue: Float
    private val saturation: Float

    init {
        val (h, s, _) = hsl(seed)
        hue = h
        saturation = s.coerceIn(MIN_SATURATION, MAX_SATURATION)
    }

    val primary = TonalPalette(hue, saturation)
    val secondary = TonalPalette(hue, saturation * SECONDARY_SATURATION)
    val tertiary = TonalPalette(hue + TERTIARY_HUE_SHIFT, saturation * TERTIARY_SATURATION)
    val neutral = TonalPalette(hue, NEUTRAL_SATURATION)
    val neutralVariant = TonalPalette(hue, NEUTRAL_VARIANT_SATURATION)
    val error = TonalPalette(ERROR_HUE, ERROR_SATURATION)

    private companion object {
        const val MIN_SATURATION = 0.45f
        const val MAX_SATURATION = 0.9f
        const val SECONDARY_SATURATION = 0.4f
        const val TERTIARY_SATURATION = 0.6f
        const val TERTIARY_HUE_SHIFT = 60f
        const val NEUTRAL_SATURATION = 0.08f
        const val NEUTRAL_VARIANT_SATURATION = 0.14f
        const val ERROR_HUE = 3f
        const val ERROR_SATURATION = 0.75f
    }
}

/** Tone of the dark scheme's primary: a little more vivid than Material's 80, like the Talos orange. */
internal const val DARK_PRIMARY_TONE = 75

/**
 * A full Material 3 color scheme generated from [seed], light or dark, with the tones of
 * the Material baseline scheme (so the usual contrast between a color and its "on" color).
 */
fun seedColorScheme(seed: Int, dark: Boolean): ColorScheme {
    val p = SeedPalettes(seed)
    return if (dark) darkScheme(p) else lightScheme(p)
}

private fun lightScheme(p: SeedPalettes): ColorScheme = lightColorScheme(
    primary = p.primary[40],
    onPrimary = p.primary[100],
    primaryContainer = p.primary[90],
    onPrimaryContainer = p.primary[10],
    inversePrimary = p.primary[80],
    secondary = p.secondary[40],
    onSecondary = p.secondary[100],
    secondaryContainer = p.secondary[90],
    onSecondaryContainer = p.secondary[10],
    tertiary = p.tertiary[40],
    onTertiary = p.tertiary[100],
    tertiaryContainer = p.tertiary[90],
    onTertiaryContainer = p.tertiary[10],
    background = p.neutral[98],
    onBackground = p.neutral[10],
    surface = p.neutral[98],
    onSurface = p.neutral[10],
    surfaceVariant = p.neutralVariant[90],
    onSurfaceVariant = p.neutralVariant[30],
    surfaceTint = p.primary[40],
    inverseSurface = p.neutral[20],
    inverseOnSurface = p.neutral[95],
    error = p.error[40],
    onError = p.error[100],
    errorContainer = p.error[90],
    onErrorContainer = p.error[10],
    outline = p.neutralVariant[50],
    outlineVariant = p.neutralVariant[80],
    scrim = p.neutral[0],
    surfaceBright = p.neutral[98],
    surfaceDim = p.neutral[87],
    surfaceContainerLowest = p.neutral[100],
    surfaceContainerLow = p.neutral[96],
    surfaceContainer = p.neutral[94],
    surfaceContainerHigh = p.neutral[92],
    surfaceContainerHighest = p.neutral[90],
)

private fun darkScheme(p: SeedPalettes): ColorScheme = darkColorScheme(
    primary = p.primary[DARK_PRIMARY_TONE],
    onPrimary = p.primary[20],
    primaryContainer = p.primary[30],
    onPrimaryContainer = p.primary[90],
    inversePrimary = p.primary[40],
    secondary = p.secondary[80],
    onSecondary = p.secondary[20],
    secondaryContainer = p.secondary[30],
    onSecondaryContainer = p.secondary[90],
    tertiary = p.tertiary[80],
    onTertiary = p.tertiary[20],
    tertiaryContainer = p.tertiary[30],
    onTertiaryContainer = p.tertiary[90],
    background = p.neutral[6],
    onBackground = p.neutral[90],
    surface = p.neutral[6],
    onSurface = p.neutral[90],
    surfaceVariant = p.neutralVariant[30],
    onSurfaceVariant = p.neutralVariant[80],
    surfaceTint = p.primary[DARK_PRIMARY_TONE],
    inverseSurface = p.neutral[90],
    inverseOnSurface = p.neutral[20],
    error = p.error[80],
    onError = p.error[20],
    errorContainer = p.error[30],
    onErrorContainer = p.error[90],
    outline = p.neutralVariant[60],
    outlineVariant = p.neutralVariant[30],
    scrim = p.neutral[0],
    surfaceBright = p.neutral[24],
    surfaceDim = p.neutral[6],
    surfaceContainerLowest = p.neutral[4],
    surfaceContainerLow = p.neutral[10],
    surfaceContainer = p.neutral[12],
    surfaceContainerHigh = p.neutral[17],
    surfaceContainerHighest = p.neutral[22],
)
