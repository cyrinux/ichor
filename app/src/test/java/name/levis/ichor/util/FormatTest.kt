package name.levis.ichor.util

import org.junit.Assert.assertEquals
import org.junit.Test

class FormatTest {

    @Test
    fun bytes() {
        assertEquals("0 B", formatBytes(0))
        assertEquals("1023 B", formatBytes(1023))
        assertEquals("1.5 KiB", formatBytes(1536))
        assertEquals("31.1 GiB", formatBytes(33_347_887_104))
        assertEquals("474.5 GiB", formatBytes(509_534_134_272))
    }

    @Test
    fun duration() {
        assertEquals("<1m", formatDuration(59))
        assertEquals("42m", formatDuration(42 * 60))
        assertEquals("5h 12m", formatDuration(5 * 3600 + 12 * 60))
        assertEquals("3d 4h", formatDuration(3 * 86_400 + 4 * 3600 + 59))
    }

    @Test
    fun fraction() {
        assertEquals(0f, usedFraction(0, 0))
        assertEquals(0.6f, usedFraction(100, 40), 0.0001f)
        assertEquals(1f, usedFraction(100, -5), 0.0001f)
        assertEquals(0f, usedFraction(100, 500), 0.0001f)
    }

    @Test
    fun days() {
        val now = 1_000_000_000_000L
        assertEquals(10, daysUntil(now / 1000 + 10 * 86_400, now))
        assertEquals(-1, daysUntil(now / 1000 - 60, now))
    }
}
