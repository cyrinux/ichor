package name.levis.ichor.data

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.channels.Channel
import name.levis.ichor.model.KubeSanitizeResult
import name.levis.ichor.model.PodSweepProgress
import name.levis.ichor.model.SanitizeFailure
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class PodSweepManagerTest {
    private class FakeSweep {
        val events = Channel<PodSweepEvent>(Channel.UNLIMITED)
        var stops = 0
        var started: Pair<String, String>? = null

        fun progress(done: Int, total: Int, failed: Int = 0) = events.trySend(PodSweepEvent.Progress(PodSweepProgress(done, total, "web/pod-$done", failed)))

        fun done(result: KubeSanitizeResult, error: String? = null) {
            events.trySend(PodSweepEvent.Done(result, error))
            events.close()
        }
    }

    private fun manager(fake: FakeSweep, onStarted: () -> Unit = {}) = PodSweepManager(
        startRun = { namespace, names ->
            fake.started = namespace to names
            PodSweepHandle(fake.events) { fake.stops++ }
        },
        onStarted = onStarted,
        scope = CoroutineScope(SupervisorJob() + Dispatchers.Unconfined),
    )

    @Test
    fun aSweepCountsItsPodsThenEndsWithItsResult() {
        val fake = FakeSweep()
        var notified = 0
        val m = manager(fake) { notified++ }
        assertTrue(m.start("admin@test", "web", "web/a\nweb/b\n\nweb/c"))
        assertEquals("web" to "web/a\nweb/b\n\nweb/c", fake.started)
        assertEquals(1, notified)
        assertEquals(3, m.current.value?.total)
        assertFalse("one sweep at a time", m.start("admin@test", "web", "web/d"))

        fake.progress(2, 3, failed = 1)
        val running = m.current.value!!
        assertTrue(running.running)
        assertEquals(2, running.done)
        assertEquals(1, running.deleted)
        assertEquals(1, running.failedCount)

        fake.done(KubeSanitizeResult(deleted = 2, failed = listOf(SanitizeFailure("web/b", "the pod no longer exists"))))
        val ended = m.current.value!!
        assertTrue(ended.finished)
        assertEquals(3, ended.done)
        assertEquals(2, ended.deleted)
        assertEquals(listOf("web/b"), ended.failures.map { it.name })
        assertNull(ended.error)
        assertTrue("a new sweep once it ended", m.start("admin@test", "web", "web/b"))
    }

    @Test
    fun stopAsksTheCoreOnceAndEndsStopped() {
        val fake = FakeSweep()
        val m = manager(fake)
        m.start("admin@test", "", "web/a\ndb/b")
        m.stop()
        assertEquals(1, fake.stops)
        assertTrue(m.current.value!!.stopping)

        fake.done(KubeSanitizeResult(deleted = 1, stopped = true))
        assertTrue(m.current.value!!.stopped)
        m.stop()
        assertEquals("no run left to stop", 1, fake.stops)
    }

    @Test
    fun nothingToDeleteStartsNothing() {
        val fake = FakeSweep()
        val m = manager(fake)
        assertFalse(m.start("admin@test", "web", "\n \n"))
        assertNull(fake.started)
        assertNull(m.current.value)
    }

    @Test
    fun aRefusedSweepEndsWithItsError() {
        val fake = FakeSweep()
        val m = manager(fake)
        m.start("demo", "demo", "demo/a")
        fake.done(KubeSanitizeResult(), error = "this action is unavailable in the demo cluster")
        assertEquals("this action is unavailable in the demo cluster", m.current.value?.error)
    }
}
