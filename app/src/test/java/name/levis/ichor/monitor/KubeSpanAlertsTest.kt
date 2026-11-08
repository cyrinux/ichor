package name.levis.ichor.monitor

import kotlinx.serialization.json.Json
import name.levis.ichor.data.TalosJson
import name.levis.ichor.model.KubeNodesOverview
import name.levis.ichor.model.KubeSpanMeshHealth
import name.levis.ichor.model.NodeHealth
import name.levis.ichor.model.ShareTarget
import name.levis.ichor.widget.StatusItem
import name.levis.ichor.widget.statusItems
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class KubeSpanAlertsTest {
    private val now = 1_800_000_000_000L
    private val key = "node-a|node-b"
    private val value = "critical|neverHandshaked|0|node-a|node-b"

    private fun health(issues: Map<String, String>, checked: Boolean = true, partial: Boolean = false) =
        KubeSpanMeshHealth("lab", checked = checked, partial = partial, links = 1, broken = issues.size, issues = issues)

    private fun snap(issues: Map<String, String>?, watched: Boolean = true) = ClusterSnapshot(
        context = "lab",
        takenAt = now,
        nodes = mapOf("192.0.2.20" to NodeState("node-a", NodeHealth.READY)),
        etcdChecked = true,
        kubespanWatched = watched,
        kubespanChecked = watched && issues != null,
        kubespanIssues = issues.orEmpty(),
    )

    @Test
    fun theGoJsonDecodes() {
        val json = """{"context":"lab","checked":true,"partial":false,"links":3,"broken":1,"issues":{"$key":"$value"}}"""
        val h = TalosJson.decodeFromString(KubeSpanMeshHealth.serializer(), json)
        assertEquals(mapOf(key to value), kubespanIssuesOf(h))
        val d = KubeSpanDetail.parse(value)
        assertEquals(KUBESPAN_REASON_NEVER, d.reason)
        assertEquals(0L, d.since)
        assertEquals("node-b", d.hostB)
    }

    @Test
    fun aBrokenLinkNotifiesOnceThenOnceWhenBack() {
        val first = evaluate(snap(emptyMap()), snap(mapOf(key to value)), now)
        val alert = first.alerts.single()
        assertEquals(AlertKind.KUBESPAN_DOWN, alert.kind)
        assertEquals("kubespan:$key", alert.key)
        assertEquals("node-a ↔ node-b", alert.subject)
        assertEquals(DATA_CRITICAL, alert.ruleSeverity)
        assertEquals("kubespan", alert.ruleKind)

        val still = evaluate(first.next, snap(mapOf(key to value)), now)
        assertTrue(still.alerts.isEmpty())

        val back = evaluate(still.next, snap(emptyMap()), now)
        assertEquals(AlertKind.KUBESPAN_UP, back.alerts.single().kind)
        assertEquals("node-a ↔ node-b", back.alerts.single().subject)
        assertTrue(evaluate(back.next, snap(emptyMap()), now).alerts.isEmpty())
    }

    @Test
    fun theFirstCheckIsASilentBaseline() {
        val baseline = evaluate(null, snap(mapOf(key to value)), now)
        assertTrue(baseline.alerts.isEmpty())
        assertEquals(mapOf(key to value), baseline.next.kubespanIssues)
    }

    @Test
    fun notCheckedKeepsTheKnownIssues() {
        assertNull(kubespanIssuesOf(health(emptyMap(), checked = false), known = mapOf(key to value)))
        val first = evaluate(snap(emptyMap()), snap(mapOf(key to value)), now)
        val blind = evaluate(first.next, snap(null), now)
        assertTrue(blind.alerts.isEmpty())
        assertEquals(mapOf(key to value), blind.next.kubespanIssues)
    }

    @Test
    fun aPartialReadNeverSaysBack() {
        val other = "node-c|node-d"
        val issues = kubespanIssuesOf(health(mapOf(other to value), partial = true), known = mapOf(key to value))
        assertEquals(setOf(key, other), issues?.keys)
        assertEquals(emptySet<String>(), kubespanIssuesOf(health(emptyMap()), known = mapOf(key to value))?.keys)
    }

    @Test
    fun turningTheToggleOffForgetsWithoutABack() {
        val first = evaluate(snap(emptyMap()), snap(mapOf(key to value)), now)
        val off = evaluate(first.next, snap(null, watched = false), now)
        assertTrue(off.alerts.isEmpty())
        assertTrue(off.next.kubespanIssues.isEmpty())
        assertFalse(off.next.kubespanWatched)
    }

    @Test
    fun aKubeconfigClusterHasNoTrack() {
        val s = kubeSnapshotOf(KubeNodesOverview(), "lab", certNotAfter = 0, takenAt = now)
        assertFalse(s.kubespanWatched)
        assertFalse(s.kubespanChecked)
    }

    @Test
    fun itOpensTheKubeSpanScreenOnItsOwnChannel() {
        val alert = evaluate(snap(emptyMap()), snap(mapOf(key to value)), now).alerts.single()
        assertEquals(ShareTarget.screen(ShareTarget.KUBESPAN), alert.shareTarget())
        assertEquals(AlertChannel.MESH, alert.channel)
    }

    @Test
    fun repeatsAndTheWidgetSeeIt() {
        val next = evaluate(snap(emptyMap()), snap(mapOf(key to value)), now).next
        assertEquals(listOf(AlertKind.KUBESPAN_DOWN), firingAlerts(next).map { it.kind })
        assertEquals(StatusItem.TunnelsDown(1), statusItems(next).first())
    }

    @Test
    fun anOldSnapshotDecodes() {
        val json = Json { ignoreUnknownKeys = true }
        val s = json.decodeFromString(ClusterSnapshot.serializer(), """{"context":"lab","takenAt":1,"nodes":{}}""")
        assertFalse(s.kubespanWatched)
        assertTrue(s.kubespanIssues.isEmpty())
    }

    @Test
    fun theFleetEntryCarriesIt() {
        val state = MonitorState(clusters = mapOf("k" to evaluate(snap(emptyMap()), snap(mapOf(key to value)), now).next))
        val entry = fleetEntries(state, null, emptyMap(), masked = false).single()
        assertTrue(entry.kubespanChecked)
        assertEquals(mapOf(key to value), entry.kubespanIssues)
    }
}
