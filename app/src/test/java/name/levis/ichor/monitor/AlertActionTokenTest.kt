package name.levis.ichor.monitor

import name.levis.ichor.data.MemoryPrefs
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.Base64

class AlertActionTokenTest {
    @Test
    fun theTokenIsMadeOnceAndKept() {
        val prefs = MemoryPrefs()
        val token = AlertActionToken(prefs).value()
        assertEquals(32, Base64.getDecoder().decode(token).size)
        assertEquals(token, AlertActionToken(prefs).value())
        assertNotEquals(token, AlertActionToken(MemoryPrefs()).value())
    }

    @Test
    fun onlyTheInstallsTokenIsAccepted() {
        val store = AlertActionToken(MemoryPrefs())
        val token = store.value()
        assertTrue(store.accepts(token))
        assertFalse(store.accepts(null))
        assertFalse(store.accepts(""))
        assertFalse(store.accepts(token.dropLast(1)))
        assertFalse(store.accepts(AlertActionToken(MemoryPrefs()).value()))
    }

    @Test
    fun nothingIsAcceptedBeforeATokenWasMade() {
        assertFalse(AlertActionToken(MemoryPrefs()).accepts(""))
        assertFalse(AlertActionToken(MemoryPrefs()).accepts("c29tZXRoaW5n"))
    }

    @Test
    fun snoozePruningKeepsTheToken() {
        val prefs = MemoryPrefs()
        val token = AlertActionToken(prefs).value()
        AlertSnoozes(prefs).apply {
            snooze("fp", "node:10.0.0.2", until = 1_000)
            prune(now = 2_000)
        }
        assertTrue(AlertActionToken(prefs).accepts(token))
    }
}
