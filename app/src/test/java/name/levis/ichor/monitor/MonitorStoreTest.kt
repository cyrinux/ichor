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
    private val other = ClusterSnapshot(context = "lab", takenAt = 2_000, nodes = emptyMap(), fingerprint = "fp2")
    private val legacyJson = TalosJson.encodeToString(ClusterSnapshot.serializer(), snapshot)

    @Test
    fun eachClusterIsKeptInTheSealedFileOnly() {
        val prefs = MemoryPrefs()
        val sealed = MemoryValue()
        val state = MonitorState(mapOf("fp" to snapshot, "fp2" to other), mapOf("fp2" to Reach(2)), active = "fp")
        MonitorStore(prefs, sealed).saveState(state)
        val read = MonitorStore(prefs, sealed)
        assertEquals(state, read.state.value)
        assertEquals(other, read.snapshot("fp2"))
        assertTrue(prefs.all.isEmpty())
    }

    @Test
    fun aCountWithoutAnySnapshotIsKept() {
        val sealed = MemoryValue()
        val state = MonitorState(emptyMap(), mapOf("fp" to Reach(3, notified = true)))
        MonitorStore(MemoryPrefs(), sealed).saveState(state)
        assertEquals(state, MonitorStore(MemoryPrefs(), sealed).state.value)
    }

    @Test
    fun clearingDeletesThemAll() {
        val sealed = MemoryValue()
        val store = MonitorStore(MemoryPrefs(), sealed).apply {
            saveState(MonitorState(mapOf("fp" to snapshot)))
            clearSnapshots()
        }
        assertNull(sealed.value)
        assertEquals(MonitorState.EMPTY, store.state.value)
    }

    @Test
    fun theSingleSnapshotOfAnOlderVersionGoesToItsCluster() {
        val sealed = MemoryValue().apply { write(legacyJson) }
        val store = MonitorStore(MemoryPrefs(), sealed)
        // Compared with its own previous snapshot: the first check after the update is no baseline.
        assertEquals(snapshot, store.snapshot("fp"))
        assertEquals("fp", store.state.value.active)
    }

    @Test
    fun aLegacySnapshotWithoutFingerprintIsDropped() {
        val json = TalosJson.encodeToString(ClusterSnapshot.serializer(), snapshot.copy(fingerprint = ""))
        assertEquals(MonitorState.EMPTY, decodeMonitorState(json))
    }

    @Test
    fun anUnreadableFileIsNoState() {
        assertNull(decodeMonitorState("{\"nope\":1}"))
    }

    @Test
    fun aPlaintextSnapshotIsMovedAndTheOtherSettingsStay() {
        val prefs = MemoryPrefs().apply {
            edit().putString("snapshot", legacyJson).putBoolean("alerts_enabled", true).commit()
        }
        val sealed = MemoryValue()
        val store = MonitorStore(prefs, sealed)
        assertEquals(snapshot, store.snapshot("fp"))
        assertFalse(prefs.contains("snapshot"))
        assertTrue(store.alertsEnabled.value)
        assertTrue(sealed.value != null)
    }

    @Test
    fun withTheKeystoreUnavailableThePlaintextSnapshotStays() {
        val prefs = MemoryPrefs().apply { edit().putString("snapshot", legacyJson).commit() }
        val store = MonitorStore(prefs, MemoryValue().apply { failWrites = true })
        assertTrue(prefs.contains("snapshot"))
        assertEquals(snapshot, store.snapshot("fp")) // the widget still has it
    }

    @Test
    fun unreachableAlertsAreOffAndCountThreeByDefault() {
        val store = MonitorStore(MemoryPrefs(), MemoryValue())
        assertFalse(store.unreachableAlerts.value)
        assertEquals(3, store.unreachableRuns.value)
        store.setUnreachableRuns(42)
        assertEquals(10, store.unreachableRuns.value)
        store.setUnreachableRuns(1)
        assertEquals(2, store.unreachableRuns.value)
    }
}
