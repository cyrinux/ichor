package name.levis.talosmobile.model

import kotlin.math.abs
import kotlin.math.cbrt
import kotlin.math.pow
import kotlin.math.roundToInt

/**
 * Main colors (ARGB) handed out to clusters as they are imported, far apart in hue so two
 * clusters are told apart at a glance. The first is the Talos orange: a single cluster
 * looks like the app always did.
 */
val CLUSTER_SEEDS: List<Int> = listOf(
    0xFFFF7A45, // orange
    0xFF2A78D6, // blue
    0xFF2E9E6B, // green
    0xFF8E5BD9, // purple
    0xFFD6336C, // pink
    0xFF0E9AA7, // teal
    0xFFC29A12, // yellow
    0xFF5C6BC0, // indigo
).map { it.toInt() }

val DEFAULT_CLUSTER_SEED: Int = CLUSTER_SEEDS.first()

/**
 * The color of each cluster of [fingerprints] (see ContextSummary.fingerprint): the [saved]
 * one when it has one, else the first of [CLUSTER_SEEDS] no other cluster uses (they repeat
 * past that many clusters). Clusters that are gone are dropped.
 */
fun assignClusterColors(saved: Map<String, Int>, fingerprints: List<String>): Map<String, Int> {
    val known = fingerprints.filter { it.isNotBlank() }.distinct()
    val kept = known.mapNotNull { fp -> saved[fp]?.let { fp to it } }.toMap()
    return known.fold(kept) { colors, fp ->
        if (fp in colors) return@fold colors
        val free = CLUSTER_SEEDS.firstOrNull { it !in colors.values } ?: CLUSTER_SEEDS[colors.size % CLUSTER_SEEDS.size]
        colors + (fp to free)
    }
}

/** A cluster's main color among [this] (colors by fingerprint); the default one while unknown. */
fun Map<String, Int>.seedOf(context: ContextSummary?): Int =
    context?.let { this[it.fingerprint] } ?: DEFAULT_CLUSTER_SEED

/** HSL hue of an ARGB color, in degrees [0, 360). Greys have none: 0. */
fun hueOf(argb: Int): Float = hsl(argb).first

/** A vivid color of [hue] (degrees), what the hue slider picks as a cluster's main color. */
fun seedFromHue(hue: Float): Int = hslToArgb(hue, PICKED_SATURATION, PICKED_LIGHTNESS)

private const val PICKED_SATURATION = 0.8f
private const val PICKED_LIGHTNESS = 0.55f

/** Hue (degrees), saturation and lightness (0..1) of an ARGB color. */
internal fun hsl(argb: Int): Triple<Float, Float, Float> {
    val r = (argb shr 16 and 0xFF) / 255f
    val g = (argb shr 8 and 0xFF) / 255f
    val b = (argb and 0xFF) / 255f
    val max = maxOf(r, g, b)
    val min = minOf(r, g, b)
    val delta = max - min
    val lightness = (max + min) / 2
    if (delta == 0f) return Triple(0f, 0f, lightness)
    val saturation = delta / (1 - abs(2 * lightness - 1))
    val sector = when (max) {
        r -> ((g - b) / delta).mod(6f)
        g -> (b - r) / delta + 2
        else -> (r - g) / delta + 4
    }
    return Triple((sector * 60).mod(360f), saturation, lightness)
}

internal fun hslToArgb(hue: Float, saturation: Float, lightness: Float): Int {
    val chroma = (1 - abs(2 * lightness - 1)) * saturation
    val sector = hue.mod(360f) / 60
    val x = chroma * (1 - abs(sector.mod(2f) - 1))
    val (r, g, b) = when (sector.toInt()) {
        0 -> Triple(chroma, x, 0f)
        1 -> Triple(x, chroma, 0f)
        2 -> Triple(0f, chroma, x)
        3 -> Triple(0f, x, chroma)
        4 -> Triple(x, 0f, chroma)
        else -> Triple(chroma, 0f, x)
    }
    val m = lightness - chroma / 2
    fun channel(v: Float) = ((v + m) * 255).roundToInt().coerceIn(0, 255)
    return (0xFF shl 24) or (channel(r) shl 16) or (channel(g) shl 8) or channel(b)
}

/** Relative luminance (WCAG) of an ARGB color, 0..1. */
internal fun luminance(argb: Int): Double {
    fun linear(channel: Int): Double {
        val c = channel / 255.0
        return if (c <= 0.04045) c / 12.92 else ((c + 0.055) / 1.055).pow(2.4)
    }
    return 0.2126 * linear(argb shr 16 and 0xFF) + 0.7152 * linear(argb shr 8 and 0xFF) + 0.0722 * linear(argb and 0xFF)
}

/** Perceived lightness (CIE L*, 0..100): what Material calls a color's tone. */
internal fun toneOf(argb: Int): Double {
    val y = luminance(argb)
    return if (y > 216.0 / 24389.0) 116 * cbrt(y) - 16 else y * 24389.0 / 27.0
}

/** WCAG contrast ratio between two ARGB colors (1..21). */
internal fun contrast(a: Int, b: Int): Double {
    val (dark, light) = listOf(luminance(a), luminance(b)).sorted()
    return (light + 0.05) / (dark + 0.05)
}

/**
 * The color of [hue] and [saturation] whose perceived lightness is [tone] (0 black, 100
 * white). Two tones are always as far apart to the eye whatever the hue, which is what keeps
 * text readable on every generated palette; plain HSL lightness does not (yellow at 50% is
 * far brighter than blue at 50%). Found by bisection: L* grows with the HSL lightness.
 */
internal fun tonal(hue: Float, saturation: Float, tone: Int): Int {
    if (tone <= 0) return BLACK
    if (tone >= 100) return WHITE
    var low = 0f
    var high = 1f
    repeat(TONE_SEARCH_STEPS) {
        val mid = (low + high) / 2
        if (toneOf(hslToArgb(hue, saturation, mid)) < tone) low = mid else high = mid
    }
    return hslToArgb(hue, saturation, (low + high) / 2)
}

private const val TONE_SEARCH_STEPS = 16
private const val BLACK = 0xFF000000.toInt()
private const val WHITE = 0xFFFFFFFF.toInt()
