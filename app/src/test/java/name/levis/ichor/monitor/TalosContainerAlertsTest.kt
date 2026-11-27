package name.levis.ichor.monitor

import name.levis.ichor.data.TalosJson
import name.levis.ichor.model.NodeHealth
import name.levis.ichor.model.ShareTarget
import name.levis.ichor.model.TalosContainersHealth
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class TalosContainerAlertsTest {
    private val now = 1_800_000_000_000L
    private val restartingKey = "192.0.2.20|sensor-agent"
    private val restarting = "warning|backoff|12|1|edge-1|sensor-agent"
    private val failedKey = "192.0.2.21|backup"
    private val failed = "critical|failed|0|3|edge-2|backup"

    private fun snap(issues: Map<String, String>?, watched: Boolean = true) = ClusterSnapshot(
        context = "lab",
        takenAt = now,
        nodes = mapOf("192.0.2.20" to NodeState("edge-1", NodeHealth.READY)),
        etcdChecked = true,
        talosContainerWatched = watched,
        talosContainerChecked = watched && issues != null,
        talosContainerIssues = issues.orEmpty(),
    )

    @Test
    fun theGoJsonDecodes() {
        val json = """{"context":"lab","checked":true,"unread":[],"issues":{"$restartingKey":"$restarting"}}"""
        val health = TalosJson.decodeFromString(TalosContainersHealth.serializer(), json)
        assertEquals(mapOf(restartingKey to restarting), talosContainerIssuesOf(health))
        val d = TalosContainerDetail.parse(restarting)
        assertEquals(12L, d.restartCount)
        assertEquals(1, d.exitCode)
        assertEquals("edge-1", d.hostname)
        assertEquals("sensor-agent", d.name)
        assertEquals(false, d.failed)
        assertTrue(TalosContainerDetail.parse(failed).failed)
    }

    @Test
    fun aFailedContainerAlertsAtOnceAndWhenRunningAgain() {
        val first = evaluate(snap(emptyMap()), snap(mapOf(failedKey to failed)), now)
        val alert = first.alerts.single()
        assertEquals(AlertKind.TALOS_CONTAINER_PROBLEM, alert.kind)
        assertEquals("taloscontainer:$failedKey", alert.key)
        assertEquals("backup", alert.subject)
        assertEquals(DATA_CRITICAL, alert.ruleSeverity)
        assertEquals("taloscontainer", alert.ruleKind)
        assertEquals(AlertChannel.NODES, alert.channel)
        assertEquals(ShareTarget(target = ShareTarget.NODE, addr = "192.0.2.21", host = "edge-2"), alert.shareTarget())

        assertTrue(evaluate(first.next, snap(mapOf(failedKey to failed)), now).alerts.isEmpty())

        val back = evaluate(first.next, snap(emptyMap()), now)
        assertEquals(AlertKind.TALOS_CONTAINER_OK, back.alerts.single().kind)
    }

    @Test
    fun aRestartingContainerAlertsOnItsSecondCheck() {
        val once = evaluate(snap(emptyMap()), snap(mapOf(restartingKey to restarting)), now)
        assertTrue(once.alerts.isEmpty())
        val twice = evaluate(once.next, snap(mapOf(restartingKey to restarting)), now)
        assertEquals(AlertKind.TALOS_CONTAINER_PROBLEM, twice.alerts.single().kind)
        assertEquals(DATA_WARNING, twice.alerts.single().ruleSeverity)
    }

    @Test
    fun anUnreadNodeKeepsItsIssuesAndNothingReadIsUnknown() {
        val known = mapOf(restartingKey to restarting, failedKey to failed)
        val partial = TalosContainersHealth("lab", checked = true, unread = listOf("192.0.2.21"), issues = emptyMap())
        assertEquals(mapOf(failedKey to failed), talosContainerIssuesOf(partial, known))
        assertNull(talosContainerIssuesOf(TalosContainersHealth("lab", checked = false), known))
    }

    @Test
    fun watchingOffForgetsAndFiringListsThem() {
        val off = evaluate(snap(mapOf(failedKey to failed)), snap(null, watched = false), now)
        assertTrue(off.alerts.isEmpty())
        assertTrue(off.next.talosContainerIssues.isEmpty())
        assertEquals(listOf("taloscontainer:$failedKey"), firingAlerts(snap(mapOf(failedKey to failed))).map { it.key })
    }
}
