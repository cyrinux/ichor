package name.levis.ichor.model

import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.toArgb
import name.levis.ichor.ui.theme.seedColorScheme
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The home summary's cluster-coloured roles stay legible whatever the cluster's color: the
 * name on the primary container (text, WCAG AA 4.5:1) and the stat icons on the secondary
 * container (meaningful icons, 3:1; held to 4.5:1 too, should text ever sit there). True black
 * only darkens the surfaces, not these containers, so light and dark cover it.
 */
class SummaryColorTest {

    private val grey = 0xFF808080.toInt()
    private val seeds = CLUSTER_SEEDS + (0 until 360 step 15).map { seedFromHue(it.toFloat()) } +
        grey + 0xFF000000.toInt() + 0xFFFFFFFF.toInt() + 0xFFFFFF00.toInt() + 0xFF0000FF.toInt()

    private fun minContrast(pair: (dark: Boolean, seed: Int) -> Pair<Color, Color>): Double =
        seeds.flatMap { seed -> listOf(false, true).map { dark -> seed to dark } }
            .minOf { (seed, dark) ->
                val (background, foreground) = pair(dark, seed)
                contrast(background.toArgb(), foreground.toArgb())
            }

    @Test
    fun nameReadsOnThePrimaryContainer() {
        val min = minContrast { dark, seed -> seedColorScheme(seed, dark).let { it.primaryContainer to it.onPrimaryContainer } }
        assertTrue("onPrimaryContainer/primaryContainer min $min", min >= 4.5)
    }

    @Test
    fun iconsReadOnTheSecondaryContainer() {
        val min = minContrast { dark, seed -> seedColorScheme(seed, dark).let { it.secondaryContainer to it.onSecondaryContainer } }
        assertTrue("onSecondaryContainer/secondaryContainer min $min", min >= 3.0)
        assertTrue("onSecondaryContainer/secondaryContainer min $min, text level", min >= 4.5)
    }
}
