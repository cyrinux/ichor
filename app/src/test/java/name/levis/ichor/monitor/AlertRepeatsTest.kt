package name.levis.ichor.monitor

import name.levis.ichor.model.ALERT_ACTION_MUTE
import name.levis.ichor.model.ALERT_ACTION_REPEAT
import name.levis.ichor.model.AlertDecision
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.NodeHealth
import name.levis.ichor.model.NodeHealth.NOT_READY
import name.levis.ichor.model.NodeHealth.READY
import name.levis.ichor.model.NodeHealth.UNREACHABLE
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class AlertRepeatsTest {

    private val now = 1_800_000_000_000L
    private val minute = 60_000L
    private val prod = ContextSummary(name = "prod", fingerprint = "fp", clusterId = "prod")

    private fun snap(vararg nodes: Pair<String, NodeHealth>, context: String = "prod") = ClusterSnapshot(
        context = context,
        takenAt = now,
        nodes = nodes.associate { (addr, h) -> addr to NodeState("host-$addr", h) },
        fingerprint = "fp",
    )

    @Test
    fun firingAlertsAreTheProblemsASnapshotStillHolds() {
        val snapshot = snap("a" to READY, "b" to NOT_READY, "c" to UNREACHABLE).copy(
            etcdChecked = true,
            etcdAlarms = listOf("m1:NOSPACE"),
            amIssues = mapOf("f1" to "critical|DiskFull|shop"),
            amPending = listOf("f2"),
        )
        val keys = firingAlerts(snapshot).map { it.key }
        assertEquals(listOf("node:b", "node:c", "etcd:m1:NOSPACE", "am:f1"), keys)
        assertTrue(firingAlerts(snapshot).all { it.problem })
    }

    @Test
    fun alertTimesFollowTheFiringAlerts() {
        val prev = snap("a" to NOT_READY, "b" to NOT_READY).copy(
            firstSeen = mapOf("node:a" to now - 40 * minute, "node:b" to now - 20 * minute),
            lastNotified = mapOf("node:a" to now - 16 * minute, "node:b" to now - 20 * minute),
        )
        // b recovered, c went down.
        val next = withAlertTimes(prev, snap("a" to UNREACHABLE, "b" to READY, "c" to NOT_READY), now)
        assertEquals(mapOf("node:a" to now - 40 * minute, "node:c" to now), next.firstSeen)
        assertEquals(mapOf("node:a" to now - 16 * minute), next.lastNotified)
    }

    @Test
    fun anotherContextIsABaseline() {
        val prev = snap("a" to NOT_READY, context = "old").copy(firstSeen = mapOf("node:a" to 1), lastNotified = mapOf("node:a" to 1))
        val next = withAlertTimes(prev, snap("a" to NOT_READY), now)
        assertEquals(mapOf("node:a" to now), next.firstSeen)
        assertTrue(next.lastNotified.isEmpty())
    }

    @Test
    fun onlyPostedUnchangedAlertsOfAReadClusterAreOngoing() {
        val snapshot = snap("a" to NOT_READY, "b" to NOT_READY, "c" to NOT_READY).copy(
            firstSeen = mapOf("node:a" to 1, "node:b" to 1, "node:c" to 1),
            // c was never posted (a baseline, or muted): it does not repeat.
            lastNotified = mapOf("node:a" to 1, "node:b" to 1),
        )
        val state = MonitorState(mapOf("fp" to snapshot))
        val read = ClusterRead(prod, snapshot)
        // b changed this run: posted as it is now, not repeated.
        val changed = listOf(ClusterAlerts(prod, listOf(Alert("node:b", AlertKind.NODE_UNREACHABLE, true))))
        val ongoing = ongoingAlerts(listOf(read), state, changed)
        assertEquals(listOf("node:a"), ongoing.single().alerts.map { it.key })
        assertTrue(ongoingAlerts(listOf(read.copy(skipped = true)), state, emptyList()).isEmpty())
        assertTrue(ongoingAlerts(listOf(read.copy(snapshot = null)), state, emptyList()).isEmpty())
    }

    @Test
    fun repeatInputsCarryTheTimesAndTheAcknowledgement() {
        val snapshot = snap("a" to NOT_READY).copy(firstSeen = mapOf("node:a" to 10), lastNotified = mapOf("node:a" to 20))
        val ongoing = listOf(ClusterAlerts(prod, firingAlerts(snapshot)))
        val input = repeatInputs(ongoing, MonitorState(mapOf("fp" to snapshot)), { _, _ -> false }, { c, k -> c == "fp" && k == "node:a" }).single()
        assertEquals("node", input.kind)
        assertTrue(input.ongoing && input.problem && input.acknowledged)
        assertEquals(10L, input.firstSeenMillis)
        assertEquals(20L, input.lastNotifiedMillis)
    }

    @Test
    fun onlyRepeatDecisionsArePostedAgain() {
        val snapshot = snap("a" to NOT_READY, "b" to NOT_READY).copy(
            firstSeen = mapOf("node:a" to now - 25 * minute, "node:b" to now - 25 * minute),
            lastNotified = mapOf("node:a" to now - 15 * minute, "node:b" to now - 5 * minute),
        )
        val ongoing = listOf(ClusterAlerts(prod, firingAlerts(snapshot)))
        val inputs = repeatInputs(ongoing, MonitorState(mapOf("fp" to snapshot)), { _, _ -> false }, { _, _ -> false })
        val decisions = listOf(
            AlertDecision("fp", "node:a", ALERT_ACTION_REPEAT, repeats = true),
            AlertDecision("fp", "node:b", ALERT_ACTION_MUTE, reason = "notDue"),
        )
        val posts = repeatPlan(ongoing, inputs, decisions, now)
        assertEquals(listOf("node:a"), posts.map { it.alert.key })
        assertEquals(25L, posts.single().minutes)
        // No decisions (no rules, or Go refused them): nothing repeats.
        assertTrue(repeatPlan(ongoing, inputs, null, now).isEmpty())
        // A decision for another key is not trusted.
        assertTrue(repeatPlan(ongoing, inputs, listOf(AlertDecision("fp", "node:x", ALERT_ACTION_REPEAT)), now).isEmpty())
    }

    @Test
    fun postedProblemsSetTheirLastNotifiedTime() {
        val snapshot = snap("a" to NOT_READY, "b" to READY).copy(firstSeen = mapOf("node:a" to 1))
        val state = MonitorState(mapOf("fp" to snapshot))
        val posted = listOf(
            ClusterAlerts(
                prod,
                listOf(Alert("node:a", AlertKind.NODE_NOT_READY, true), Alert("node:b", AlertKind.NODE_READY, false)),
            ),
        )
        val next = withNotified(state, posted, now)
        assertEquals(mapOf("node:a" to now), next.clusters.getValue("fp").lastNotified)
        assertEquals(state, withNotified(state, emptyList(), now))
    }

    @Test
    fun stillFiringMinutesAreWholeAndAtLeastOne() {
        assertEquals(25L, stillFiringMinutes(now - 25 * minute - 30_000, now))
        assertEquals(1L, stillFiringMinutes(now - 10_000, now))
        assertEquals(1L, stillFiringMinutes(0, now))
    }

    @Test
    fun firingKeysSkipAClusterNotRead() {
        val snapshot = snap("a" to NOT_READY)
        val state = MonitorState(mapOf("fp" to snapshot))
        assertEquals(mapOf("fp" to setOf("node:a")), firingKeys(listOf(ClusterRead(prod, snapshot)), state))
        assertTrue(firingKeys(listOf(ClusterRead(prod, null)), state).isEmpty())
    }
}
