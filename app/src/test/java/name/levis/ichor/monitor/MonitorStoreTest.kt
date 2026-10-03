package name.levis.ichor.monitor

import name.levis.ichor.data.MemoryPrefs
import name.levis.ichor.data.MemoryValue
import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class MonitorStoreTest {

    private val snapshot = ClusterSnapshot(context = "prod", takenAt = 1_000, nodes = emptyMap(), fingerprint = "fp")

    @Test
    fun theSnapshotIsKeptInTheSealedFileOnly() {
        val prefs = MemoryPrefs()
        val sealed = MemoryValue()
        MonitorStore(prefs, sealed).saveSnapshot(snapshot)
        assertEquals(snapshot, MonitorStore(prefs, sealed).snapshot())
        assertTrue(prefs.all.isEmpty())
    }

    @Test
    fun clearingDeletesIt() {
        val sealed = MemoryValue()
        MonitorStore(MemoryPrefs(), sealed).apply {
            saveSnapshot(snapshot)
            clearSnapshot()
        }
        assertNull(sealed.value)
    }

    @Test
    fun aPlaintextSnapshotIsMovedAndTheOtherSettingsStay() {
        val prefs = MemoryPrefs().apply {
            edit().putString("snapshot", TalosJson.encodeToString(ClusterSnapshot.serializer(), snapshot))
                .putBoolean("alerts_enabled", true).commit()
        }
        val sealed = MemoryValue()
        val store = MonitorStore(prefs, sealed)
        assertEquals(snapshot, store.snapshot())
        assertFalse(prefs.contains("snapshot"))
        assertTrue(store.alertsEnabled.value)
        assertTrue(sealed.value != null)
    }

    @Test
    fun withTheKeystoreUnavailableThePlaintextSnapshotStays() {
        val prefs = MemoryPrefs().apply {
            edit().putString("snapshot", TalosJson.encodeToString(ClusterSnapshot.serializer(), snapshot)).commit()
        }
        val store = MonitorStore(prefs, MemoryValue().apply { failWrites = true })
        assertTrue(prefs.contains("snapshot"))
        assertEquals(snapshot, store.snapshot()) // the widget still has it
    }
}
