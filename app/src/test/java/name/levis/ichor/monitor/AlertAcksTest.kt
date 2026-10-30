package name.levis.ichor.monitor

import name.levis.ichor.data.MemoryPrefs
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class AlertAcksTest {

    @Test
    fun anAcknowledgementIsKeptHashedPerClusterAndKey() {
        val prefs = MemoryPrefs()
        AlertAcks(prefs).acknowledge("fp", "node:10.0.0.2", at = 1_000)
        val acks = AlertAcks(prefs)
        assertTrue(acks.isAcknowledged("fp", "node:10.0.0.2"))
        assertFalse(acks.isAcknowledged("other", "node:10.0.0.2"))
        assertFalse(acks.isAcknowledged("fp", "node:10.0.0.3"))
        assertFalse(prefs.all.keys.single().contains("10.0.0.2"))
    }

    @Test
    fun aBlankClusterOrKeyIsNeverAcknowledged() {
        val acks = AlertAcks(MemoryPrefs())
        acks.acknowledge("", "node:a", at = 1)
        acks.acknowledge("fp", "", at = 1)
        assertFalse(acks.isAcknowledged("", "node:a"))
        assertFalse(acks.isAcknowledged("fp", ""))
    }

    @Test
    fun aRecoveredAlertsAcknowledgementEnds() {
        val acks = AlertAcks(MemoryPrefs())
        acks.acknowledge("fp", "node:a", at = 1)
        acks.acknowledge("fp", "node:b", at = 1)
        acks.acknowledge("fl", "node:a", at = 1)
        acks.retainFiring("fp", listOf("node:b"))
        assertFalse(acks.isAcknowledged("fp", "node:a"))
        assertTrue(acks.isAcknowledged("fp", "node:b"))
        // Another cluster's are untouched.
        assertTrue(acks.isAcknowledged("fl", "node:a"))
    }

    @Test
    fun aRemovedClustersAcknowledgementsGo() {
        val acks = AlertAcks(MemoryPrefs())
        acks.acknowledge("fp", "node:a", at = 1)
        acks.acknowledge("gone", "node:a", at = 1)
        acks.retain(listOf("fp"))
        assertTrue(acks.isAcknowledged("fp", "node:a"))
        assertFalse(acks.isAcknowledged("gone", "node:a"))
    }
}
