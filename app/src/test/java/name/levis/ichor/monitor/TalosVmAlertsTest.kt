package name.levis.ichor.monitor

import kotlinx.serialization.json.Json
import name.levis.ichor.data.TalosJson
import name.levis.ichor.model.KubeNodesOverview
import name.levis.ichor.model.NodeHealth
import name.levis.ichor.model.ShareTarget
import name.levis.ichor.model.TalosVmProblem
import name.levis.ichor.model.TalosVmProblemNode
import name.levis.ichor.model.TalosVmProblemReport
import name.levis.ichor.widget.StatusItem
import name.levis.ichor.widget.statusItems
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class TalosVmAlertsTest {
    private val now = 1_800_000_000_000L
    private val node = "192.0.2.20"
    private val key = "$node|vm|app"
    private val value = "critical|vmError|node-a|app|domain name is occupied by another VM"

    private fun report(vararg nodes: TalosVmProblemNode) = TalosVmProblemReport("lab", nodes.toList())

    private fun nodeWith(vararg problems: TalosVmProblem, error: String = "", errors: Map<String, String> = emptyMap()) =
        TalosVmProblemNode(node, "node-a", supported = true, problems = problems.toList(), errors = errors, error = error)

    private fun snap(issues: Map<String, String>?, watched: Boolean = true) = ClusterSnapshot(
        context = "lab",
        takenAt = now,
        nodes = mapOf(node to NodeState("node-a", NodeHealth.READY)),
        etcdChecked = true,
        vmWatched = watched,
        vmChecked = watched && issues != null,
        vmIssues = issues.orEmpty(),
    )

    @Test
    fun theGoJsonDecodes() {
        val json = """{"context":"lab","nodes":[{"node":"$node","hostname":"node-a","supported":true,"reason":"",
            "vms":[{"name":"app"}],"libraries":[],"errors":{},
            "problems":[{"node":"$node","vm":"app","kind":"vmError","detail":"domain name is occupied by another VM"}]}]}"""
        val r = TalosJson.decodeFromString(TalosVmProblemReport.serializer(), json)
        assertEquals(mapOf(key to value), talosVmIssuesOf(r))
        val d = TalosVmDetail.parse(value)
        assertEquals(TALOS_VM_ERROR, d.kind)
        assertEquals("node-a", d.hostname)
        assertEquals("app", d.vm)
    }

    @Test
    fun aVmStoppedOnPurposeRaisesNothing() {
        // Go reports no problem for it: the node has VMs, none of them a problem.
        assertTrue(talosVmIssuesOf(report(nodeWith())).isEmpty())
    }

    @Test
    fun aSeparatorInTheTextNeverShiftsTheFields() {
        val p = TalosVmProblem(node, "app", TALOS_VM_DOWN, "a|b")
        val d = TalosVmDetail.parse(talosVmIssuesOf(report(nodeWith(p))).getValue(key))
        assertEquals("app", d.vm)
        assertEquals("a/b", d.detail)
    }

    @Test
    fun anUnreadNodeKeepsItsKnownIssues() {
        val known = mapOf(key to value)
        assertEquals(known, talosVmIssuesOf(report(nodeWith(error = "timeout")), known))
        assertEquals(known, talosVmIssuesOf(report(nodeWith(errors = mapOf("vms" to "permission denied"))), known))
        // Another section failing still judges the VMs.
        assertTrue(talosVmIssuesOf(report(nodeWith(errors = mapOf("libraries" to "x"))), known).isEmpty())
    }

    @Test
    fun aProblemNotifiesOnceThenOnceWhenBack() {
        val first = evaluate(snap(emptyMap()), snap(mapOf(key to value)), now)
        val alert = first.alerts.single()
        assertEquals(AlertKind.TALOS_VM_PROBLEM, alert.kind)
        assertEquals("vm:$key", alert.key)
        assertEquals("app", alert.subject)
        assertEquals(DATA_CRITICAL, alert.ruleSeverity)
        assertEquals("vm", alert.ruleKind)

        val still = evaluate(first.next, snap(mapOf(key to value)), now)
        assertTrue(still.alerts.isEmpty())

        val back = evaluate(still.next, snap(emptyMap()), now)
        assertEquals(AlertKind.TALOS_VM_OK, back.alerts.single().kind)
        assertTrue(evaluate(back.next, snap(emptyMap()), now).alerts.isEmpty())
    }

    @Test
    fun notCheckedKeepsTheKnownIssues() {
        val first = evaluate(snap(emptyMap()), snap(mapOf(key to value)), now)
        val blind = evaluate(first.next, snap(null), now)
        assertTrue(blind.alerts.isEmpty())
        assertEquals(mapOf(key to value), blind.next.vmIssues)
        assertEquals(mapOf(key to value), knownTalosVmIssues(blind.next, "lab"))
        assertTrue(knownTalosVmIssues(blind.next, "other").isEmpty())
    }

    @Test
    fun turningTheToggleOffForgetsWithoutABack() {
        val first = evaluate(snap(emptyMap()), snap(mapOf(key to value)), now)
        val off = evaluate(first.next, snap(null, watched = false), now)
        assertTrue(off.alerts.isEmpty())
        assertTrue(off.next.vmIssues.isEmpty())
        assertFalse(off.next.vmWatched)
    }

    @Test
    fun aKubeconfigClusterHasNoTrack() {
        val s = kubeSnapshotOf(KubeNodesOverview(), "lab", certNotAfter = 0, takenAt = now)
        assertFalse(s.vmWatched)
        assertFalse(s.vmChecked)
    }

    @Test
    fun itOpensTheTalosVmListOnTheNodeChannel() {
        val alert = evaluate(snap(emptyMap()), snap(mapOf(key to value)), now).alerts.single()
        assertEquals(ShareTarget.screen(ShareTarget.TALOS_VMS), alert.shareTarget())
        assertEquals(AlertChannel.NODES, alert.channel)
    }

    @Test
    fun repeatsTheWidgetAndTheFleetSeeIt() {
        val next = evaluate(snap(emptyMap()), snap(mapOf(key to value)), now).next
        assertEquals(listOf(AlertKind.TALOS_VM_PROBLEM), firingAlerts(next).map { it.kind })
        assertEquals(StatusItem.VmsDown(1), statusItems(next).first())
        val entry = fleetEntries(MonitorState(clusters = mapOf("k" to next)), null, emptyMap(), masked = false).single()
        assertTrue(entry.vmChecked)
        assertEquals(mapOf(key to value), entry.vmIssues)
    }

    @Test
    fun anOldSnapshotDecodes() {
        val json = Json { ignoreUnknownKeys = true }
        val s = json.decodeFromString(ClusterSnapshot.serializer(), """{"context":"lab","takenAt":1,"nodes":{}}""")
        assertFalse(s.vmWatched)
        assertTrue(s.vmIssues.isEmpty())
    }
}
