package name.levis.ichor.data

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.receiveAsFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import name.levis.ichor.model.ConfigTryCommand
import name.levis.ichor.model.ConfigTryEvent
import name.levis.ichor.model.ConfigTryProgress
import name.levis.ichor.model.ConfigTryState
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class ConfigTryManagerTest {
    /** A try run the test drives: [events] go to the manager, the commands it sends land in [sent]. */
    private class FakeTry {
        val events = Channel<ConfigTryEvent>(Channel.UNLIMITED)
        val sent = mutableListOf<ConfigTryCommand>()
        var starts = 0
        private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Unconfined)

        fun run(commands: Flow<ConfigTryCommand>): Flow<ConfigTryEvent> {
            starts++
            scope.launch { commands.collect { sent += it } }
            return events.receiveAsFlow()
        }
    }

    private val trying = ConfigTryEvent.Progress(ConfigTryProgress(phase = ConfigTryState.TRYING, deadline = 120_000))

    private fun manager(fake: FakeTry, started: () -> Unit = {}) = ConfigTryManager(
        runTry = { _, _, _, _, commands -> fake.run(commands) },
        scope = CoroutineScope(SupervisorJob() + Dispatchers.Unconfined),
        onStarted = started,
    )

    @Test
    fun keepGoesOutOnceWhileTheNodeWaits() = runBlocking {
        val fake = FakeTry()
        var started = 0
        val m = manager(fake) { started++ }

        assertTrue(m.start("10.0.0.2", "w-1", "base", "draft", 300))
        assertEquals(1, started)

        // Applying: nothing to keep yet.
        m.keep()
        assertTrue(fake.sent.isEmpty())

        fake.events.send(trying)
        assertTrue(m.current.value!!.trying)
        assertEquals(120_000L, m.current.value!!.deadline)

        m.keep()
        m.keep() // the phase is keeping now: the second one goes nowhere
        m.revert()
        assertEquals(listOf(ConfigTryCommand.KEEP), fake.sent)
        assertEquals(ConfigTryState.KEEPING, (m.current.value!!.state as ConfigTryState.Running).phase)

        fake.events.send(ConfigTryEvent.Done("kept", ""))
        assertEquals(ConfigTryState.Kept, m.current.value!!.state)
        assertTrue(m.current.value!!.finished)
        assertNull(m.current.value!!.error)

        m.dismiss()
        assertNull(m.current.value)
    }

    @Test
    fun oneTryAtATime() = runBlocking {
        val fake = FakeTry()
        val m = manager(fake)

        assertTrue(m.start("10.0.0.2", "w-1", "base", "draft", 60))
        assertFalse(m.start("10.0.0.3", "w-2", "base", "draft", 60))
        assertEquals(1, fake.starts)

        // A running try is not forgotten.
        m.dismiss()
        assertEquals("10.0.0.2", m.current.value?.node)
    }

    @Test
    fun aRunEndingWithoutAnOutcomeIsAFailure() = runBlocking {
        val fake = FakeTry()
        val m = manager(fake)

        m.start("10.0.0.2", "w-1", "base", "draft", 60)
        fake.events.send(trying)
        fake.events.close()

        val state = m.current.value!!
        assertTrue(state.finished)
        assertEquals("", state.error)
    }

    @Test
    fun aFailedTryCarriesItsMessage() = runBlocking {
        val fake = FakeTry()
        val m = manager(fake)

        m.start("10.0.0.2", "w-1", "base", "draft", 60)
        fake.events.send(ConfigTryEvent.Done("", "the node refused the config"))

        assertEquals("the node refused the config", m.current.value?.error)
        // Another try can start once this one ended.
        assertTrue(m.start("10.0.0.2", "w-1", "base", "draft", 60))
    }
}
