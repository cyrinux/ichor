package name.levis.ichor.monitor

import name.levis.ichor.data.MemoryPrefs
import name.levis.ichor.data.MemoryValue
import name.levis.ichor.model.ALERT_REASON_QUIET
import name.levis.ichor.model.ALERT_REASON_SEVERITY
import name.levis.ichor.model.ALERT_SEVERITY_CRITICAL
import name.levis.ichor.model.AlertRule
import name.levis.ichor.model.AlertRules
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Test

class AlertMutesTest {
    private val quiet = AlertMute(ALERT_REASON_QUIET, until = 7_000, at = 1_000)

    @Test
    fun theLatestMuteOfAKeyIsKeptHashed() {
        val prefs = MemoryPrefs()
        val mutes = AlertMutes(prefs)
        mutes.record("fp", "node:10.0.0.2", quiet)
        assertEquals(quiet, AlertMutes(prefs).of("fp", "node:10.0.0.2"))
        assertFalse(prefs.all.keys.single().contains("10.0.0.2"))
        val later = AlertMute(ALERT_REASON_SEVERITY, until = 0, at = 2_000)
        mutes.record("fp", "node:10.0.0.2", later)
        assertEquals(later, mutes.of("fp", "node:10.0.0.2"))
        assertNull(mutes.of("other", "node:10.0.0.2"))
    }

    @Test
    fun aMuteShowsOnTheAlertOpenAtThatTime() {
        val mutes = AlertMutes(MemoryPrefs())
        mutes.record("fp", "am:x", quiet)
        assertEquals(quiet, mutes.during("fp", "am:x", from = 1_000, to = null))
        assertEquals(quiet, mutes.during("fp", "am:x", from = 500, to = 1_500))
        // Opened again after it: that one was not muted.
        assertNull(mutes.during("fp", "am:x", from = 1_001, to = null))
        assertNull(mutes.during("fp", "am:x", from = 0, to = 999))
    }

    @Test
    fun oldMutesAndRemovedClustersGo() {
        val prefs = MemoryPrefs()
        val mutes = AlertMutes(prefs)
        mutes.record("fp", "am:old", quiet)
        mutes.record("fp", "am:new", quiet.copy(at = AlertMutes.MUTE_KEEP_MILLIS + 5_000))
        mutes.record("gone", "am:new", quiet.copy(at = AlertMutes.MUTE_KEEP_MILLIS + 5_000))
        mutes.prune(now = AlertMutes.MUTE_KEEP_MILLIS + 6_000)
        assertNull(mutes.of("fp", "am:old"))
        mutes.retain(listOf("fp"))
        assertNull(mutes.of("gone", "am:new"))
        assertEquals(1, prefs.all.size)
    }

    @Test
    fun blankClustersOrKeysAreNotStored() {
        val prefs = MemoryPrefs()
        AlertMutes(prefs).apply {
            record("", "am:x", quiet)
            record("fp", "", quiet)
        }
        assertEquals(0, prefs.all.size)
    }

    @Test
    fun theStoreKeepsTheRulesAndForgetsRemovedClusters() {
        val prefs = MemoryPrefs()
        val rules = AlertRules(clusters = mapOf("fp" to AlertRule(minSeverity = ALERT_SEVERITY_CRITICAL), "gone" to AlertRule(digest = true)))
        MonitorStore(prefs, MemoryValue()).setAlertRules(rules)
        val read = MonitorStore(prefs, MemoryValue())
        assertEquals(rules, read.alertRules.value)
        read.retainAlertRules(listOf("fp"))
        assertEquals(setOf("fp"), MonitorStore(prefs, MemoryValue()).alertRules.value.clusters.keys)
    }

    @Test
    fun unreadableStoredRulesAreNone() {
        assertEquals(AlertRules.NONE, decodeAlertRules("""{"default":{"quietHours":5}}"""))
        assertEquals(AlertRules.NONE, decodeAlertRules(null))
    }
}
