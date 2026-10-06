package name.levis.ichor.data

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertThrows
import org.junit.Test

class RetryTest {

    private val pauses = mutableListOf<Long>()

    private fun <T> run(delays: List<Long>, block: suspend () -> T): T =
        runBlocking { retrying(delays, pause = { pauses += it }, block = block) }

    @Test
    fun firstSuccessNeedsNoRetry() {
        assertEquals("ok", run(listOf(1, 2)) { "ok" })
        assertEquals(emptyList<Long>(), pauses)
    }

    @Test
    fun triesAgainAfterAFailure() {
        var calls = 0
        // The Keystore busy twice, e.g. while the app starts after an update.
        val result = run(listOf(1, 2, 3)) { if (++calls < 3) error("busy") else "ok" }
        assertEquals("ok", result)
        assertEquals(listOf(1L, 2L), pauses)
    }

    @Test
    fun aStoredNothingIsAnAnswer() {
        var calls = 0
        assertEquals(null, run<String?>(listOf(1, 2)) { calls++; null })
        assertEquals(1, calls)
    }

    @Test
    fun throwsTheLastFailure() {
        var calls = 0
        val thrown = assertThrows(IllegalStateException::class.java) { run(listOf(1, 2)) { error("failure ${++calls}") } }
        assertEquals("failure 3", thrown.message)
        assertEquals(listOf(1L, 2L), pauses)
    }

    @Test
    fun cancellationIsNotRetried() {
        var calls = 0
        assertThrows(CancellationException::class.java) { run(listOf(1, 2)) { calls++; throw CancellationException("gone") } }
        assertEquals(1, calls)
    }
}
