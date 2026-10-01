package name.levis.talosmobile.data

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class SupportPromptTest {

    private val day = 86_400_000L
    private val start = 1_000_000_000_000L

    private fun state(launches: Int = 20, firstSeen: Long = start, lastAsked: Long = 0, never: Boolean = false) =
        SupportState(firstSeen = firstSeen, launches = launches, lastAsked = lastAsked, never = never)

    @Test
    fun waitsForRealUse() {
        assertFalse("too new", state().shouldAsk(start + 13 * day))
        assertFalse("too few launches", state(launches = 9).shouldAsk(start + 30 * day))
        assertTrue(state().shouldAsk(start + 14 * day))
    }

    @Test
    fun atMostEveryNinetyDays() {
        val asked = state(lastAsked = start + 20 * day)
        assertFalse(asked.shouldAsk(start + 100 * day))
        assertTrue(asked.shouldAsk(start + 110 * day))
    }

    @Test
    fun neverMeansNever() {
        assertFalse(state(never = true).shouldAsk(start + 1_000 * day))
    }
}
