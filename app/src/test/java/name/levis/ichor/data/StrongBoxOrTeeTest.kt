package name.levis.ichor.data

import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertThrows
import org.junit.Test

class StrongBoxOrTeeTest {

    private val pauses = mutableListOf<Long>()
    private var discarded = 0

    private fun pick(strongBox: (() -> String)?, tee: () -> String = { "tee" }): String =
        runBlocking { strongBoxOrTee(strongBox, tee, listOf(1, 2), pause = { pauses += it }, discard = { discarded++ }) }

    @Test
    fun noChipGoesToTheTee() {
        assertEquals("tee", pick(null))
        assertEquals(emptyList<Long>(), pauses)
    }

    @Test
    fun aChipThatAnswersKeepsTheKey() {
        assertEquals("strongbox", pick({ "strongbox" }))
        assertEquals(0, discarded)
    }

    @Test
    fun waitsForTheChipToReconnect() {
        var calls = 0
        // UNKNOWN_ERROR twice while the secure element's connection is down, then it answers.
        assertEquals("strongbox", pick({ if (++calls < 3) throw java.security.ProviderException("Keystore key generation failed") else "strongbox" }))
        assertEquals(listOf(1L, 2L), pauses)
    }

    @Test
    fun aChipThatNeverAnswersFallsBackToTheTee() {
        var calls = 0
        assertEquals("tee", pick({ calls++; throw java.security.ProviderException("Keystore key generation failed") }))
        assertEquals(3, calls)
        assertEquals(1, discarded)
    }

    @Test
    fun aTeeFailureCarriesTheChipOne() {
        val thrown = assertThrows(IllegalStateException::class.java) {
            pick({ throw java.security.ProviderException("chip") }, tee = { error("tee") })
        }
        assertEquals("chip", thrown.suppressed.single().message)
    }
}
