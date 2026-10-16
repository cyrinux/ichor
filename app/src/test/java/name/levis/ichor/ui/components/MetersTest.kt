package name.levis.ichor.ui.components

import androidx.compose.ui.graphics.Color
import name.levis.ichor.ui.theme.StatusColors
import org.junit.Assert.assertEquals
import org.junit.Test

class MetersTest {
    private val colors = StatusColors(ok = Color.Green, warn = Color.Yellow, bad = Color.Red, muted = Color.Gray)

    @Test
    fun usageColorBelowTheThresholdIsOk() {
        assertEquals(colors.ok, usageColor(0f, 0.75f, colors))
        assertEquals(colors.ok, usageColor(0.74f, 0.75f, colors))
    }

    @Test
    fun usageColorFromTheThresholdIsWarn() {
        assertEquals(colors.warn, usageColor(0.75f, 0.75f, colors))
        assertEquals(colors.warn, usageColor(0.89f, 0.75f, colors))
    }

    @Test
    fun usageColorFromNinetyPercentIsBad() {
        assertEquals(colors.bad, usageColor(0.9f, 0.75f, colors))
        assertEquals(colors.bad, usageColor(1f, 0.75f, colors))
    }

    @Test
    fun usageColorFollowsACustomThreshold() {
        assertEquals(colors.ok, usageColor(0.79f, 0.8f, colors))
        assertEquals(colors.warn, usageColor(0.8f, 0.8f, colors))
        // A threshold past 90 % never hides a full bar.
        assertEquals(colors.bad, usageColor(0.92f, 0.95f, colors))
    }
}
