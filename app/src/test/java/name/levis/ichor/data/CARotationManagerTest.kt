package name.levis.ichor.data

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.channels.Channel
import name.levis.ichor.model.CARotationProgress
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class CARotationManagerTest {
    private class FakeRotation {
        val events = Channel<CARotationEvent>(Channel.UNLIMITED)
        var proceeds = 0
        var stops = 0

        fun progress(phase: String, step: Int) = events.trySend(CARotationEvent.Progress(CARotationProgress(phase = phase, step = step, total = 5, message = phase)))

        fun talosconfig(yaml: String, final: Boolean) = events.trySend(CARotationEvent.Talosconfig(yaml, final))

        fun done(error: String? = null) {
            events.trySend(CARotationEvent.Done(error))
            events.close()
        }
    }

    private class Store(val failing: Set<String> = emptySet()) {
        val stored = mutableListOf<Pair<String, String>>()

        suspend fun store(context: String, yaml: String) {
            if (yaml in failing) throw IllegalStateException("the keystore is locked")
            stored += context to yaml
        }
    }

    private fun manager(fake: FakeRotation, store: Store, onStarted: () -> Unit = {}) = CARotationManager(
        startRun = { CARotationHandle(fake.events, { fake.proceeds++ }, { fake.stops++ }) },
        store = store::store,
        activeContext = { "admin@prod" },
        onStarted = onStarted,
        scope = CoroutineScope(SupervisorJob() + Dispatchers.Unconfined),
    )

    @Test
    fun theTransitionConfigIsStoredBeforeTheRunGoesOnThenTheFinalOne() {
        val fake = FakeRotation()
        val store = Store()
        var notified = 0
        val m = manager(fake, store) { notified++ }

        assertTrue(m.start())
        assertEquals(1, notified)
        assertFalse("one rotation at a time", m.start())

        fake.progress("accept", 2)
        assertEquals(0, fake.proceeds)

        fake.talosconfig("transition", final = false)
        assertEquals(listOf("admin@prod" to "transition"), store.stored)
        assertEquals("the run goes on once stored", 1, fake.proceeds)
        assertTrue(m.current.value!!.transitionStored)

        fake.progress("swap", 4)
        fake.talosconfig("final", final = true)
        assertEquals(1, fake.proceeds)
        fake.done()

        val ended = m.current.value!!
        assertTrue(ended.finished)
        assertTrue(ended.finalStored)
        assertNull(ended.error)
        assertEquals("admin@prod", ended.hostname)
        assertEquals(listOf("accept", "swap"), ended.events.map { it.phase })
        assertEquals(listOf("transition", "final"), store.stored.map { it.second })
    }

    @Test
    fun aTransitionConfigThatCannotBeStoredStopsTheRun() {
        val fake = FakeRotation()
        val m = manager(fake, Store(failing = setOf("transition")))
        m.start()

        fake.talosconfig("transition", final = false)
        assertEquals("never proceed without the phone's config", 0, fake.proceeds)
        assertEquals(1, fake.stops)
        assertEquals("the keystore is locked", m.current.value!!.error)

        fake.done("stopped: the cluster keeps the CAs it has at this step")
        val ended = m.current.value!!
        assertTrue(ended.finished)
        assertEquals("the storing failure is the reason kept", "the keystore is locked", ended.error)
        assertFalse(ended.transitionStored)
    }

    @Test
    fun aFinalConfigThatCannotBeStoredIsANoteNotAFailure() {
        val fake = FakeRotation()
        val m = manager(fake, Store(failing = setOf("final")))
        m.start()
        fake.talosconfig("transition", final = false)
        fake.talosconfig("final", final = true)
        fake.done()

        val ended = m.current.value!!
        assertNull(ended.error)
        assertFalse(ended.finalStored)
        assertEquals("the keystore is locked", ended.finalStoreError)
    }

    @Test
    fun stopAsksTheCoreOnceAndDismissForgetsOnlyAnEndedRun() {
        val fake = FakeRotation()
        val m = manager(fake, Store())
        m.start()
        m.dismiss()
        assertTrue("a running rotation stays", m.current.value != null)

        m.stop()
        assertTrue(m.current.value!!.stopping)
        assertEquals(1, fake.stops)

        fake.done("stopped")
        m.stop()
        assertEquals("no run left to stop", 1, fake.stops)
        m.dismiss()
        assertNull(m.current.value)
    }
}
