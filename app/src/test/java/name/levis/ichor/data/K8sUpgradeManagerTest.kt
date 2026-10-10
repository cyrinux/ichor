package name.levis.ichor.data

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class K8sUpgradeManagerTest {
    private class FakeRun {
        var progress: (String) -> Unit = {}
        var done: (String) -> Unit = {}
        var cancels = 0
        val dryRuns = mutableListOf<Boolean>()
    }

    private fun manager(fake: FakeRun) = K8sUpgradeManager(startRun = { _, dryRun, onProgress, onDone ->
        fake.dryRuns += dryRun
        fake.progress = onProgress
        fake.done = onDone
        K8sUpgradeHandle { fake.cancels++ }
    })

    @Test
    fun followsADryRunThenTheRealOne() {
        val fake = FakeRun()
        val m = manager(fake)

        assertTrue(m.start("1.35.1", "lab", dryRun = true))
        assertFalse(m.start("1.35.1", "lab", dryRun = false))

        fake.progress("""{"phase":"controlplane","index":0,"total":3,"node":"10.0.0.1","hostname":"cp-1","component":"apiserver","dryRun":true}""")
        assertEquals("cp-1", m.current.value?.latest?.name)

        fake.done("")
        assertTrue(m.current.value!!.finished)
        assertNull(m.current.value!!.error)

        m.dismiss()
        assertTrue(m.start("1.35.1", "lab", dryRun = false))
        assertEquals(listOf(true, false), fake.dryRuns)
    }

    @Test
    fun cancelIsShownAtOnceAndSentOnce() {
        val fake = FakeRun()
        val m = manager(fake)
        m.start("1.35.1", "lab", dryRun = false)

        m.cancel()
        assertTrue(m.current.value!!.cancelling)

        fake.done("cancelled: the nodes done so far keep the new version, the others were not touched")
        m.cancel() // finished: nothing more
        assertEquals(1, fake.cancels)
        assertTrue(m.current.value!!.error!!.startsWith("cancelled"))
    }
}
