package name.levis.ichor.data

import name.levis.ichor.model.ConfigSummary
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.SeenMac
import name.levis.ichor.model.WolTarget
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class WakeOnLanStoreTest {

    private val target = WolTarget(mac = "aa:bb:cc:dd:ee:ff", broadcast = "10.0.0.255", port = 9)
    private val macs = listOf(SeenMac("eth0", "aa:bb:cc:dd:ee:01"))

    @Test
    fun targetsAndSeenMacsSurviveARestart() {
        val sealed = MemoryValue()
        WakeOnLanStore(sealed).apply {
            set("fp", "10.0.0.2", target)
            record("fp", "10.0.0.3", macs)
        }
        val restarted = WakeOnLanStore(sealed)
        assertEquals(mapOf("fp|10.0.0.2" to target), restarted.targets.value)
        assertEquals(mapOf("fp|10.0.0.3" to macs), restarted.seen.value)
    }

    @Test
    fun nothingLeftDeletesTheFile() {
        val sealed = MemoryValue()
        WakeOnLanStore(sealed).apply {
            set("fp", "10.0.0.2", target)
            set("fp", "10.0.0.2", null)
        }
        assertNull(sealed.value)
    }

    @Test
    fun removedClustersAreForgotten() {
        val sealed = MemoryValue()
        WakeOnLanStore(sealed).apply {
            set("gone", "10.0.0.2", target)
            set("kept", "10.0.0.3", target)
            sync(ConfigSummary("kept", listOf(ContextSummary(name = "kept", fingerprint = "kept"))))
        }
        assertEquals(setOf("kept|10.0.0.3"), WakeOnLanStore(sealed).targets.value.keys)
    }

    @Test
    fun plaintextPreferencesAreMovedThenCleared() {
        val targets = MemoryPrefs().apply { edit().putString("fp|10.0.0.2", "aa:bb:cc:dd:ee:ff|10.0.0.255|9").commit() }
        val seen = MemoryPrefs().apply { edit().putString("fp|10.0.0.3", "eth0=aa:bb:cc:dd:ee:01").commit() }
        val sealed = MemoryValue()
        WakeOnLanStore.migrate(sealed, targets, seen)
        assertTrue(targets.all.isEmpty())
        assertTrue(seen.all.isEmpty())
        val store = WakeOnLanStore(sealed)
        assertEquals(mapOf("fp|10.0.0.2" to target), store.targets.value)
        assertEquals(mapOf("fp|10.0.0.3" to macs), store.seen.value)
    }

    @Test
    fun withTheKeystoreUnavailableThePreferencesStayForALaterStart() {
        val targets = MemoryPrefs().apply { edit().putString("fp|10.0.0.2", "aa:bb:cc:dd:ee:ff|10.0.0.255|9").commit() }
        val sealed = MemoryValue().apply { failWrites = true }
        WakeOnLanStore.migrate(sealed, targets, MemoryPrefs())
        assertEquals(1, targets.all.size)
    }

    @Test
    fun aFailedWriteKeepsTheChangeInMemory() {
        val store = WakeOnLanStore(MemoryValue().apply { failWrites = true })
        store.set("fp", "10.0.0.2", target)
        assertEquals(target, store.targets.value["fp|10.0.0.2"])
    }
}
