package name.levis.ichor.data

import org.junit.Assert.assertEquals
import org.junit.Test

class SkippedTalosUpdatesTest {

    @Test
    fun remembersTheReleaseSkippedPerCluster() {
        val prefs = MemoryPrefs()
        val skipped = SkippedTalosUpdates(prefs)
        skipped.set("fp1", "v1.14.2")
        skipped.set("fp2", " v1.13.9 ")
        skipped.set("", "v1.14.2")

        // Read back after a restart.
        val again = SkippedTalosUpdates(prefs)
        assertEquals(mapOf("fp1" to "v1.14.2", "fp2" to "v1.13.9"), again.versions.value)

        // The next release replaces it; undoing forgets it.
        again.set("fp1", "v1.14.3")
        again.set("fp2", null)
        assertEquals(mapOf("fp1" to "v1.14.3"), again.versions.value)
        again.set("fp1", "")
        assertEquals(emptyMap<String, String>(), SkippedTalosUpdates(prefs).versions.value)
    }
}
