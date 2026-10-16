package name.levis.ichor.data

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.channels.Channel
import name.levis.ichor.model.NETPERF_PATH_POD
import name.levis.ichor.model.NETPERF_PHASE_TESTING
import name.levis.ichor.model.NETPERF_THROUGHPUT
import name.levis.ichor.model.NetPerfProgress
import name.levis.ichor.model.NetPerfReport
import name.levis.ichor.model.NetPerfResult
import name.levis.ichor.model.NetPerfSetup
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class NetPerfManagerTest {
    private val setup = NetPerfSetup(server = "w-2", client = "w-1", seconds = 5)

    private class FakeRun {
        var events = Channel<NetPerfEvent>(Channel.UNLIMITED)
        var stops = 0
        val saved = mutableListOf<Pair<String, NetPerfReport>>()

        /** The core's Done after the namespace is gone; the channel closes after it. */
        fun done(report: NetPerfReport, error: String? = null) {
            events.trySend(NetPerfEvent.Done(report, error))
            events.close()
        }
    }

    private fun manager(fake: FakeRun) = NetPerfManager(
        startRun = {
            fake.events = Channel(Channel.UNLIMITED)
            NetPerfHandle(fake.events) { fake.stops++ }
        },
        save = { scope, report -> fake.saved += scope to report },
        scope = CoroutineScope(SupervisorJob() + Dispatchers.Unconfined),
    )

    private val measured = NetPerfReport(server = "w-2", client = "w-1", started = 1, results = listOf(NetPerfResult(NETPERF_PATH_POD, NETPERF_THROUGHPUT, throughputMbps = 9400.0)))

    @Test
    fun aTestRunsToItsReportAndIsSavedUnderItsScope() {
        val fake = FakeRun()
        val m = manager(fake)

        assertTrue(m.start("lab-plain", setup))
        assertFalse(m.start("lab-plain", setup))
        assertEquals("w-1 → w-2", m.current.value!!.hostname)

        fake.events.trySend(NetPerfEvent.Progress(NetPerfProgress(NETPERF_PHASE_TESTING, step = 1, steps = 2)))
        assertEquals(1, m.current.value!!.progress?.step)

        fake.done(measured)
        val run = m.current.value!!
        assertTrue(run.finished)
        assertNull(run.error)
        assertFalse(run.stopped)
        assertEquals(listOf("lab-plain" to measured), fake.saved)

        m.dismiss()
        assertNull(m.current.value)
    }

    @Test
    fun stopIsSentOnceAndIsNotAFailure() {
        val fake = FakeRun()
        val m = manager(fake)
        m.start("lab-plain", setup)

        m.stop()
        m.stop()
        assertEquals(1, fake.stops)
        assertTrue(m.current.value!!.stopping)
        // Not finished until the core deleted the namespace.
        assertTrue(m.current.value!!.running)
        m.dismiss()
        assertTrue(m.current.value != null)

        fake.done(NetPerfReport(), "context canceled")
        assertTrue(m.current.value!!.stopped)
        assertNull(m.current.value!!.error)
        // Nothing measured: nothing saved.
        assertTrue(fake.saved.isEmpty())
    }

    @Test
    fun aFailedTestKeepsWhatItMeasured() {
        val fake = FakeRun()
        val m = manager(fake)
        m.start("lab-plain", setup)
        fake.done(measured, "the netperf pod did not start")
        assertEquals("the netperf pod did not start", m.current.value!!.error)
        assertEquals(1, fake.saved.size)
    }

    @Test
    fun anUnreadySetupOrAStartFailureRunsNothing() {
        val fake = FakeRun()
        val m = manager(fake)
        assertFalse(m.start("lab-plain", NetPerfSetup()))
        assertNull(m.current.value)

        val failing = NetPerfManager(startRun = { throw IllegalStateException("no cluster") }, save = { _, _ -> })
        assertEquals("no cluster", runCatching { failing.start("lab-plain", setup) }.exceptionOrNull()?.message)
        assertNull(failing.current.value)
    }
}
