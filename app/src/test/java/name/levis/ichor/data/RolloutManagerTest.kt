package name.levis.ichor.data

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.receiveAsFlow
import name.levis.ichor.model.KubeRolloutPod
import name.levis.ichor.model.KubeRolloutStatus
import name.levis.ichor.model.KubeWorkload
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class RolloutManagerTest {
    private val web = KubeWorkload("Deployment", "shop", "web", desired = 3)

    private class FakeWatch {
        val items = Channel<StreamItem<KubeRolloutStatus>>(Channel.UNLIMITED)
        var watches = 0
    }

    private fun manager(fake: FakeWatch, started: MutableList<Unit> = mutableListOf()) = RolloutManager(
        watch = {
            fake.watches++
            fake.items.receiveAsFlow()
        },
        onStarted = { started += Unit },
        scope = CoroutineScope(SupervisorJob() + Dispatchers.Unconfined),
    )

    private fun pod(name: String, updated: Boolean, healthy: Boolean) = KubeRolloutPod(name, updated = updated, healthy = healthy)

    @Test
    fun followsTheNewPodsUntilTheRolloutIsDone() {
        val fake = FakeWatch()
        val started = mutableListOf<Unit>()
        val m = manager(fake, started)

        assertTrue(m.start(web))
        assertFalse(m.start(web))
        assertEquals(1, started.size)
        assertEquals(0, m.current.value!!.ready)

        val rolling = KubeRolloutStatus(web, pods = listOf(pod("web-new-1", true, true), pod("web-new-2", true, false), pod("web-old-1", false, true)))
        fake.items.trySend(StreamItem.Item(rolling))
        assertEquals(1, m.current.value!!.ready)
        assertEquals(3, m.current.value!!.total)
        assertTrue(m.current.value!!.running)

        fake.items.trySend(StreamItem.Item(rolling.copy(done = true)))
        val run = m.current.value!!
        assertTrue(run.finished)
        assertNull(run.error)
        assertEquals(3, run.ready)

        m.dismiss()
        assertNull(m.current.value)
    }

    @Test
    fun aStalledOrManualRolloutEndsTheFollow() {
        val fake = FakeWatch()
        val m = manager(fake)
        m.start(web)
        fake.items.trySend(StreamItem.Item(KubeRolloutStatus(web, failed = true)))
        assertTrue(m.current.value!!.finished)
        assertTrue(m.current.value!!.status!!.failed)

        m.dismiss()
        val fresh = FakeWatch()
        val other = manager(fresh)
        other.start(web)
        fresh.items.trySend(StreamItem.Item(KubeRolloutStatus(web, manual = true)))
        assertTrue(other.current.value!!.finished)
    }

    @Test
    fun aWatchThatFailsEndsWithItsError() {
        val fake = FakeWatch()
        val m = manager(fake)
        m.start(web)
        fake.items.trySend(StreamItem.Done("forbidden: cannot watch deployments"))
        assertTrue(m.current.value!!.finished)
        assertEquals("forbidden: cannot watch deployments", m.current.value!!.error)
    }

    @Test
    fun stopFollowingEndsTheRunAndIgnoresWhatComesNext() {
        val fake = FakeWatch()
        val m = manager(fake)
        m.start(web)

        m.stop()
        assertTrue(m.current.value!!.stopped)
        assertTrue(m.current.value!!.finished)
        fake.items.trySend(StreamItem.Item(KubeRolloutStatus(web, done = true)))
        assertNull(m.current.value!!.status)

        // Another restart is followed afresh.
        m.dismiss()
        assertTrue(m.start(web))
        assertEquals(2, fake.watches)
    }
}
