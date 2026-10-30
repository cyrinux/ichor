package name.levis.ichor.ui.runbooks

import org.junit.Assert.assertEquals
import org.junit.Test

class WaitProgressTest {

    @Test
    fun waitedSecondsStayWithinTheTimeout() {
        assertEquals(0L, waitedSeconds(startedAt = 10_000, now = 9_000, timeoutSeconds = 300))
        assertEquals(42L, waitedSeconds(startedAt = 10_000, now = 52_900, timeoutSeconds = 300))
        assertEquals(300L, waitedSeconds(startedAt = 0, now = 3_600_000, timeoutSeconds = 300))
    }

    @Test
    fun clockShowsMinutesAndSeconds() {
        assertEquals("0:07", minutesClock(7))
        assertEquals("5:00", minutesClock(300))
        assertEquals("30:00", minutesClock(1800))
    }
}
