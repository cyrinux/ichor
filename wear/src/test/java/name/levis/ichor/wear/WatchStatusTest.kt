package name.levis.ichor.wear

import org.junit.Assert.assertEquals
import org.junit.Test

class WatchStatusTest {

    @Test
    fun shortAgeFitsAComplication() {
        assertEquals("1m", shortAge(5_000))
        assertEquals("59m", shortAge(59 * 60_000L + 59_000))
        assertEquals("1h", shortAge(60 * 60_000L))
        assertEquals("23h", shortAge(24 * 60 * 60_000L - 1))
        assertEquals("2d", shortAge(2 * 24 * 60 * 60_000L))
    }
}
