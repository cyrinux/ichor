package name.levis.ichor.data

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.receiveAsFlow
import kotlinx.coroutines.runBlocking
import name.levis.ichor.model.ConfigApplyMode
import name.levis.ichor.model.ConfigEdit
import name.levis.ichor.model.MultiConfigEvent
import name.levis.ichor.model.MultiConfigNodeState
import name.levis.ichor.model.MultiConfigProgress
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class ConfigMultiManagerTest {
    private val edit = ConfigEdit(doc = 0, path = listOf("machine", "nodeLabels"), op = "add", key = "zone", type = "string", value = "b")

    private class FakeRun {
        val events = Channel<MultiConfigEvent>(Channel.UNLIMITED)
        val started = mutableListOf<List<String>>()
    }

    private fun manager(fake: FakeRun, onStarted: () -> Unit = {}) = ConfigMultiManager(
        runMulti = { nodes, _, _ ->
            fake.started += nodes
            fake.events.receiveAsFlow()
        },
        scope = CoroutineScope(SupervisorJob() + Dispatchers.Unconfined),
        onStarted = onStarted,
    )

    @Test
    fun followsTheRunToItsEnd() = runBlocking {
        val fake = FakeRun()
        var started = 0
        val m = manager(fake) { started++ }

        assertTrue(m.start("10.0.0.1", "lab", listOf("10.0.0.3", "10.0.0.1"), listOf(edit), ConfigApplyMode.REBOOT))
        assertEquals(1, started)
        assertEquals("lab", m.current.value?.hostname)

        val progress = MultiConfigProgress(
            phase = "rebooting", index = 0, total = 2, node = "10.0.0.3",
            nodes = listOf(MultiConfigNodeState("10.0.0.3", "w-1", MultiConfigNodeState.APPLYING), MultiConfigNodeState("10.0.0.1", "cp-1")),
        )
        fake.events.send(MultiConfigEvent.Progress(progress))
        assertEquals(progress, m.current.value?.run?.progress)
        assertTrue(m.current.value!!.running)

        // A running run is neither replaced nor forgotten.
        assertFalse(m.start("10.0.0.2", "lab", listOf("10.0.0.2"), listOf(edit), ConfigApplyMode.AUTO))
        m.dismiss()
        assertEquals("10.0.0.1", m.current.value?.origin)

        fake.events.send(MultiConfigEvent.Done(null))
        assertTrue(m.current.value!!.finished)
        assertNull(m.current.value!!.error)

        m.dismiss()
        assertNull(m.current.value)
        assertEquals(listOf(listOf("10.0.0.3", "10.0.0.1")), fake.started)
    }

    @Test
    fun aRunEndingWithoutAnOutcomeIsAFailure() = runBlocking {
        val fake = FakeRun()
        val m = manager(fake)

        m.start("10.0.0.1", "lab", listOf("10.0.0.1"), listOf(edit), ConfigApplyMode.STAGED)
        fake.events.close()

        assertEquals("", m.current.value?.error)
        assertTrue(m.current.value!!.finished)
    }

    @Test
    fun stopEndsTheRunAsStoppedNotFailed() = runBlocking {
        val fake = FakeRun()
        val m = manager(fake)

        m.start("10.0.0.1", "lab", listOf("10.0.0.3", "10.0.0.1"), listOf(edit), ConfigApplyMode.REBOOT)
        m.stop()

        val state = m.current.value!!
        assertTrue(state.finished)
        assertTrue(state.stopping)
        // The screen reads the run: stopped, not "every node has the change".
        assertTrue(state.run.stopped)
        assertNull(state.error)
        // Nothing more is read from the cancelled run, and a new one may start.
        fake.events.send(MultiConfigEvent.Done("late"))
        assertNull(m.current.value!!.error)
        assertTrue(m.start("10.0.0.2", "lab", listOf("10.0.0.2"), listOf(edit), ConfigApplyMode.AUTO))
    }

    @Test
    fun stopWithoutARunDoesNothing() {
        val m = manager(FakeRun())
        m.stop()
        assertNull(m.current.value)
    }

    @Test
    fun nothingToApplyIsNotStarted() {
        val m = manager(FakeRun())
        assertFalse(m.start("10.0.0.1", "lab", emptyList(), listOf(edit), ConfigApplyMode.AUTO))
        assertNull(m.current.value)
    }
}
