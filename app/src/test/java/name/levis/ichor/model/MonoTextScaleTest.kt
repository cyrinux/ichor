package name.levis.ichor.model

import org.junit.Assert.assertEquals
import org.junit.Test

class MonoTextScaleTest {

    @Test
    fun neverBelowTenSp() {
        assertEquals(10f / 12f, MonoTextScale.clamp(0.1f, baseSp = 12f), 1e-4f)
        assertEquals(10f, 12f * MonoTextScale.smaller(10.5f / 12f, baseSp = 12f), 1e-3f)
    }

    @Test
    fun atMostTwiceTheThemeSize() {
        assertEquals(2f, MonoTextScale.clamp(5f, baseSp = 12f), 1e-4f)
        assertEquals(2f, MonoTextScale.larger(1.95f, baseSp = 12f), 1e-4f)
    }

    @Test
    fun stepsBothWays() {
        assertEquals(1.15f, MonoTextScale.larger(1f, baseSp = 12f), 1e-4f)
        assertEquals(0.85f, MonoTextScale.smaller(1f, baseSp = 12f), 1e-4f)
    }

    @Test
    fun aBaseAlreadySmallerThanTheFloorKeepsItsOwnSize() {
        // A theme whose small body text is 9sp is not forced up, only not shrunk further.
        assertEquals(1f, MonoTextScale.clamp(0.5f, baseSp = 9f), 1e-4f)
    }
}
