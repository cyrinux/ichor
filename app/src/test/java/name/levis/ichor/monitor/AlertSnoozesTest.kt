package name.levis.ichor.monitor

import name.levis.ichor.data.MemoryPrefs
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class AlertSnoozesTest {
    private val down = Alert("node:10.0.0.2", AlertKind.NODE_UNREACHABLE, true, "worker-1", "no route")
    private val back = Alert("node:10.0.0.2", AlertKind.NODE_READY, false, "worker-1", "10.0.0.2")
    private val argo = Alert("gitops:argocd|apps/shop", AlertKind.GITOPS_PROBLEM, true, "apps/shop", "argocd|critical|failed")

    @Test
    fun aSnoozeLastsUntilItsEnd() {
        val snoozes = AlertSnoozes(MemoryPrefs())
        snoozes.snooze("fp", down.key, until = 1_000 + SNOOZE_MILLIS)
        assertTrue(snoozes.isSnoozed("fp", down.key, now = 1_000))
        assertTrue(snoozes.isSnoozed("fp", down.key, now = SNOOZE_MILLIS))
        assertFalse(snoozes.isSnoozed("fp", down.key, now = 1_000 + SNOOZE_MILLIS))
    }

    @Test
    fun itIsPerClusterAndKey() {
        val snoozes = AlertSnoozes(MemoryPrefs())
        snoozes.snooze("fp", down.key, until = 5_000)
        assertFalse(snoozes.isSnoozed("other", down.key, now = 1_000))
        assertFalse(snoozes.isSnoozed("fp", argo.key, now = 1_000))
    }

    @Test
    fun keysAreNotStoredInClear() {
        val prefs = MemoryPrefs()
        AlertSnoozes(prefs).snooze("fp", down.key, until = 5_000)
        assertEquals(1, prefs.all.size)
        assertFalse(prefs.all.keys.single().contains("10.0.0.2"))
        assertTrue(AlertSnoozes(prefs).isSnoozed("fp", down.key, now = 1_000))
    }

    @Test
    fun blankClustersOrKeysAreNotStored() {
        val prefs = MemoryPrefs()
        AlertSnoozes(prefs).apply {
            snooze("", down.key, until = 5_000)
            snooze("fp", "", until = 5_000)
        }
        assertTrue(prefs.all.isEmpty())
    }

    @Test
    fun pruningForgetsOnlyTheEndedOnes() {
        val prefs = MemoryPrefs()
        val snoozes = AlertSnoozes(prefs).apply {
            snooze("fp", down.key, until = 2_000)
            snooze("fp", argo.key, until = 9_000)
        }
        snoozes.prune(now = 2_000)
        assertEquals(1, prefs.all.size)
        assertTrue(snoozes.isSnoozed("fp", argo.key, now = 2_000))
    }

    @Test
    fun theMonitorPostsNothingForASnoozedKeyProblemOrResolved() {
        val snoozes = AlertSnoozes(MemoryPrefs()).apply { snooze("fp", down.key, until = 5_000) }
        assertEquals(listOf(argo), snoozes.unsnoozed(listOf(down, argo), "fp", now = 1_000))
        assertEquals(emptyList<Alert>(), snoozes.unsnoozed(listOf(back), "fp", now = 1_000))
        // Over, or another cluster: posted again.
        assertEquals(listOf(back), snoozes.unsnoozed(listOf(back), "fp", now = 5_000))
        assertEquals(listOf(down, argo), snoozes.unsnoozed(listOf(down, argo), "other", now = 1_000))
    }
}
