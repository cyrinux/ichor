package name.levis.ichor.data

import kotlinx.coroutines.awaitCancellation
import kotlinx.coroutines.cancelAndJoin
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.yield
import kotlinx.coroutines.flow.MutableStateFlow
import org.junit.Assert.assertEquals
import org.junit.Test

class MaskScopedTest {

    /** Lets the launched collectors run up to their next suspension. */
    private suspend fun settle() = repeat(20) { yield() }

    @Test
    fun streamStopsAndClearsWhenASwitchStartsThenRestarts() = runBlocking {
        val epoch = MutableStateFlow(0)
        val calls = mutableListOf<String>()
        val scoped = MaskScoped(epoch)
        val job = launch {
            scoped.run(clear = { calls += "clear" }) {
                calls += "start"
                try {
                    awaitCancellation()
                } finally {
                    calls += "stop"
                }
            }
        }
        settle()
        epoch.value = 1
        settle()
        epoch.value = 2
        settle()
        job.cancelAndJoin()

        assertEquals(listOf("start", "stop", "clear", "clear", "start", "stop"), calls)
    }

    @Test
    fun aSwitchMissedWhileNotCollectedStillClears() = runBlocking {
        val epoch = MutableStateFlow(0)
        val calls = mutableListOf<String>()
        val scoped = MaskScoped(epoch)
        // The screen was in the back stack during the whole switch.
        epoch.value = 2
        val job = launch { scoped.run(clear = { calls += "clear" }) { calls += "start" } }
        settle()
        job.cancelAndJoin()

        assertEquals(listOf("clear", "start"), calls)
    }

    @Test
    fun watchReportsTheStartAndTheEnd() = runBlocking {
        val epoch = MutableStateFlow(4)
        val seen = mutableListOf<Boolean>()
        val job = MaskScoped(epoch).watch(this) { seen += it }
        settle()
        epoch.value = 5
        settle()
        epoch.value = 6
        settle()
        job.cancelAndJoin()

        assertEquals(listOf(true, false), seen)
    }
}
