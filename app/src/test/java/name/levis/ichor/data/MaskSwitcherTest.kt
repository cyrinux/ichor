package name.levis.ichor.data

import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test

class MaskSwitcherTest {

    /** Records each step with whether the placeholder was up at that point. */
    private class Recorder(initial: PrivacyMask = PrivacyMask()) {
        val calls = mutableListOf<String>()
        var mask = initial
        var failReread = false
        lateinit var switcher: MaskSwitcher

        fun record(name: String) {
            calls += "$name:${switcher.transition.value}"
        }

        fun build(): MaskSwitcher = MaskSwitcher(
            current = { mask },
            dropReadData = { record("invalidate"); record("clearOffline"); record("clearSnapshots"); record("clearSearches") },
            apply = { record("applyMask"); mask = it },
            reread = { record("reparse"); if (failReread) error("parse failed") },
            reload = { record("reload") },
        ).also { switcher = it }
    }

    @Test
    fun dropsBeforeTheFlipAndRereadsAfter() = runBlocking {
        val recorder = Recorder()
        val switcher = recorder.build()

        assertTrue(switcher.switch(PrivacyMask(enabled = true)))

        assertEquals(
            listOf(
                "invalidate:true", "clearOffline:true", "clearSnapshots:true", "clearSearches:true",
                "applyMask:true", "reparse:true", "reload:true",
            ),
            recorder.calls,
        )
        assertFalse(switcher.transition.value)
        assertTrue(recorder.mask.enabled)
    }

    @Test
    fun sameMaskDoesNothing() = runBlocking {
        val recorder = Recorder(PrivacyMask(enabled = true, extraWords = "example"))
        val switcher = recorder.build()

        assertFalse(switcher.switch(PrivacyMask(enabled = true, extraWords = "example")))
        assertTrue(recorder.calls.isEmpty())
    }

    @Test
    fun aFailureNeverLeavesThePlaceholderUp() = runBlocking {
        val recorder = Recorder().apply { failReread = true }
        val switcher = recorder.build()

        try {
            switcher.switch(PrivacyMask(enabled = true))
            fail("the failure is not swallowed")
        } catch (_: IllegalStateException) {
        }

        assertFalse(switcher.transition.value)
        assertEquals("reparse:true", recorder.calls.last())
        // Calls waiting on the switch go on.
        switcher.awaitIdle()
    }
}
