package name.levis.ichor.data

import name.levis.ichor.model.ClusterUpgradeCommand
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class ClusterUpgradeManagerTest {
    /** A roll the test drives through the two callbacks it was given. */
    private class FakeRoll : ClusterUpgradeHandle {
        var progress: (String) -> Unit = {}
        var done: (String) -> Unit = {}
        val sent = mutableListOf<ClusterUpgradeCommand>()
        var starts = 0

        override fun send(command: ClusterUpgradeCommand) {
            sent += command
        }

        override fun cancel() = Unit
    }

    private fun manager(fake: FakeRoll) = ClusterUpgradeManager(startRun = { _, _, _, onProgress, onDone ->
        fake.starts++
        fake.progress = onProgress
        fake.done = onDone
        fake
    })

    @Test
    fun followsPausesAndEnds() {
        val fake = FakeRoll()
        val m = manager(fake)

        assertTrue(m.start("v1.12.1", "lab", drain = true, acknowledged = true))
        assertFalse(m.start("v1.12.1", "lab", drain = true, acknowledged = true))
        assertEquals(1, fake.starts)

        fake.progress("""{"phase":"paused","index":1,"total":3,"node":"10.0.0.2","message":"etcd on 10.0.0.2 is not healthy","nodes":[]}""")
        assertTrue(m.current.value!!.paused)

        m.resume()
        m.pause()
        assertEquals(listOf(ClusterUpgradeCommand.RESUME, ClusterUpgradeCommand.PAUSE), fake.sent)

        // Not forgotten while running.
        m.dismiss()
        assertEquals("v1.12.1", m.current.value?.version)

        fake.done("")
        assertTrue(m.current.value!!.finished)
        assertNull(m.current.value!!.error)
        assertFalse(m.current.value!!.paused)

        // Nothing goes to a finished roll.
        m.resume()
        assertEquals(2, fake.sent.size)

        m.dismiss()
        assertNull(m.current.value)
    }

    @Test
    fun abortIsShownAtOnceAndEndsWithItsMessage() {
        val fake = FakeRoll()
        val m = manager(fake)
        m.start("v1.12.1", "lab", drain = false, acknowledged = true)

        m.abort()
        assertTrue(m.current.value!!.aborting)
        assertEquals(listOf(ClusterUpgradeCommand.ABORT), fake.sent)

        fake.done("aborted: the nodes upgraded so far stay upgraded, the others were not touched")
        assertTrue(m.current.value!!.error!!.startsWith("aborted"))
    }

    @Test
    fun aRefusedStartLeavesNothing() {
        val m = ClusterUpgradeManager(startRun = { _, _, _, _, _ -> throw IllegalStateException("no config") })
        try {
            m.start("v1.12.1", "lab", drain = false, acknowledged = false)
        } catch (_: IllegalStateException) {
        }
        assertNull(m.current.value)
    }
}
