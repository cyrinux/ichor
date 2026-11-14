package name.levis.ichor.monitor

import name.levis.ichor.data.MemoryPrefs
import name.levis.ichor.model.PromSource
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class RelayStoreTest {

    private val source = PromSource(kind = "ichor-relay", namespace = "ichor-relay", service = "ichor-relay", port = 8080)

    @Test
    fun pairsArePerClusterAndSurviveARestart() {
        val prefs = MemoryPrefs()
        val store = RelayStore(prefs)
        store.update("fp1") { RelayPair(enabled = true, source = source, label = "homelab") }
        store.update("fp1") { it.copy(endpoint = "https://push.example.org/up/abc", p256dh = "pub", auth = "auth") }
        val read = RelayStore(prefs).read("fp1")
        assertEquals("homelab", read.label)
        assertTrue(read.pending)
        assertFalse(read.paired)
        assertEquals(RelayPair(), RelayStore(prefs).read("fp2"))
    }

    @Test
    fun aRegisteredEndpointIsPairedAndNoLongerPending() {
        val store = RelayStore(MemoryPrefs())
        store.update("fp1") { RelayPair(enabled = true, source = source, endpoint = "https://push.example.org/up/abc") }
        store.update("fp1") { it.copy(deviceId = "0123456789abcdef", distributor = "io.heckel.ntfy") }
        val pair = store.read("fp1")
        assertTrue(pair.paired)
        assertFalse(pair.pending)
    }

    @Test
    fun clearingOrRetainingForgetsAPair() {
        val prefs = MemoryPrefs()
        val store = RelayStore(prefs)
        store.update("fp1") { RelayPair(enabled = true) }
        store.update("fp2") { RelayPair(enabled = true) }
        store.clear("fp1")
        assertEquals(setOf("fp2"), store.pairs.value.keys)
        store.retain(listOf("fp3"))
        assertTrue(store.pairs.value.isEmpty())
        assertTrue(RelayStore(prefs).pairs.value.isEmpty())
    }

    @Test
    fun aBlankFingerprintIsNeverStored() {
        val store = RelayStore(MemoryPrefs())
        store.update(" ") { RelayPair(enabled = true) }
        assertTrue(store.pairs.value.isEmpty())
    }

    @Test
    fun anUnreadableValueStartsEmpty() {
        val prefs = MemoryPrefs()
        prefs.edit().putString("pairs", "not json").apply()
        assertTrue(RelayStore(prefs).pairs.value.isEmpty())
    }
}
