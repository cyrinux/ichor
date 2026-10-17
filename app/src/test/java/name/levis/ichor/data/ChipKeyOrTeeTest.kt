package name.levis.ichor.data

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.security.ProviderException

class ChipKeyOrTeeTest {

    private val pauses = mutableListOf<Long>()
    private var discarded = 0
    private var fellBack = false

    /** A fake key generator: StrongBox refuses [busy] times (UNKNOWN_ERROR), then answers. */
    private fun make(busy: Int, strongBox: Boolean = true, delays: List<Long> = listOf(1, 2, 3)): String {
        var refusals = 0
        val generate = { chip: Boolean ->
            if (chip && refusals++ < busy) throw ProviderException("Keystore key generation failed")
            if (chip) "strongbox" else "tee"
        }
        return chipKeyOrTee(
            strongBox, generate, delays,
            pause = { pauses += it }, discard = { discarded++ }, onTeeFallback = { fellBack = true },
        )
    }

    @Test
    fun aChipBusyTwiceThenAnsweringKeepsTheKey() {
        assertEquals("strongbox", make(busy = 2))
        assertEquals(listOf(1L, 2L), pauses)
        assertFalse(fellBack)
        assertEquals(0, discarded)
    }

    @Test
    fun aChipAlwaysBusyFallsBackToTheTeeAndSaysSo() {
        assertEquals("tee", make(busy = Int.MAX_VALUE))
        assertEquals(listOf(1L, 2L, 3L), pauses)
        assertTrue(fellBack)
        assertEquals(1, discarded)
    }

    @Test
    fun withoutDelaysTheChipGetsOneTryAndNoWait() {
        // The main thread: never a wait there.
        assertEquals("tee", make(busy = 1, delays = emptyList()))
        assertEquals(emptyList<Long>(), pauses)
        assertTrue(fellBack)
    }

    @Test
    fun noChipGoesToTheTeeWithoutANotice() {
        assertEquals("tee", make(busy = 0, strongBox = false))
        assertFalse(fellBack)
    }
}
