package name.levis.ichor.model

import name.levis.ichor.ui.theme.DARK_PRIMARY_TONE
import name.levis.ichor.ui.theme.SeedPalettes
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import kotlin.math.abs

class ClusterColorTest {

    @Test
    fun firstClusterIsTalosOrangeAndTheNextOnesDiffer() {
        val colors = assignClusterColors(emptyMap(), listOf("a", "b", "c"))
        assertEquals(DEFAULT_CLUSTER_SEED, colors["a"])
        assertEquals(3, colors.values.toSet().size)
    }

    @Test
    fun savedColorsAreKeptAndNewClustersAvoidThem() {
        val blue = CLUSTER_SEEDS[1]
        val custom = 0xFF123456.toInt()
        val colors = assignClusterColors(mapOf("b" to DEFAULT_CLUSTER_SEED, "c" to custom), listOf("a", "b", "c"))
        assertEquals(DEFAULT_CLUSTER_SEED, colors["b"])
        assertEquals(custom, colors["c"])
        // The orange is b's: the new cluster gets the next free seed.
        assertEquals(blue, colors["a"])
    }

    @Test
    fun removedClustersAreDroppedAndBlankFingerprintsIgnored() {
        val colors = assignClusterColors(mapOf("gone" to CLUSTER_SEEDS[2]), listOf("a", "", "a"))
        assertEquals(setOf("a"), colors.keys)
    }

    @Test
    fun seedsRepeatPastTheirNumber() {
        val many = List(CLUSTER_SEEDS.size + 2) { "c$it" }
        val colors = assignClusterColors(emptyMap(), many)
        assertEquals(many.size, colors.size)
        assertEquals(CLUSTER_SEEDS.toSet(), colors.values.toSet())
    }

    @Test
    fun seedOfFallsBackToTheDefault() {
        val a = ContextSummary(name = "a", fingerprint = "fa")
        assertEquals(CLUSTER_SEEDS[3], mapOf("fa" to CLUSTER_SEEDS[3]).seedOf(a))
        assertEquals(DEFAULT_CLUSTER_SEED, emptyMap<String, Int>().seedOf(a))
        assertEquals(DEFAULT_CLUSTER_SEED, mapOf("fa" to CLUSTER_SEEDS[3]).seedOf(null))
    }

    @Test
    fun hslRoundTrips() {
        CLUSTER_SEEDS.forEach { seed ->
            val (h, s, l) = hsl(seed)
            val back = hslToArgb(h, s, l)
            listOf(16, 8, 0).forEach { shift ->
                assertTrue("channel of ${seed.toUInt().toString(16)}", abs((seed shr shift and 0xFF) - (back shr shift and 0xFF)) <= 1)
            }
        }
        assertEquals(0f, hueOf(0xFF808080.toInt()))
        assertEquals(210f, hueOf(seedFromHue(210f)), 1f)
    }

    @Test
    fun tonalReachesTheAskedTone() {
        assertEquals(0xFF000000.toInt(), tonal(30f, 0.8f, 0))
        assertEquals(0xFFFFFFFF.toInt(), tonal(30f, 0.8f, 100))
        for (hue in 0 until 360 step 30) {
            for (tone in listOf(10, 40, 75, 90)) {
                assertEquals("hue $hue tone $tone", tone.toDouble(), toneOf(tonal(hue.toFloat(), 0.8f, tone)), 1.0)
            }
        }
    }

    /** Whatever the main color, text stays readable on it: WCAG AA (4.5) for every pair used. */
    @Test
    fun everyHueGivesReadableSchemes() {
        val seeds = CLUSTER_SEEDS + (0 until 360 step 15).map { seedFromHue(it.toFloat()) } + 0xFF808080.toInt()
        seeds.forEach { seed ->
            val p = SeedPalettes(seed)
            val pairs = listOf(
                // Light scheme.
                p.primary.argb(40) to p.primary.argb(100),
                p.primary.argb(90) to p.primary.argb(10),
                p.neutral.argb(98) to p.neutral.argb(10),
                p.neutral.argb(98) to p.primary.argb(40),
                p.neutral.argb(90) to p.neutralVariant.argb(30),
                // Dark scheme.
                p.primary.argb(DARK_PRIMARY_TONE) to p.primary.argb(20),
                p.primary.argb(30) to p.primary.argb(90),
                p.neutral.argb(6) to p.neutral.argb(90),
                p.neutral.argb(6) to p.primary.argb(DARK_PRIMARY_TONE),
                p.neutral.argb(22) to p.neutralVariant.argb(80),
            )
            pairs.forEach { (background, text) ->
                assertTrue(
                    "seed ${seed.toUInt().toString(16)}: ${contrast(background, text)}",
                    contrast(background, text) >= 4.5,
                )
            }
        }
    }
}
