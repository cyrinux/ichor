package name.levis.ichor.monitor

import name.levis.ichor.data.TalosJson
import name.levis.ichor.model.BgpDown
import name.levis.ichor.model.BgpNodePeers
import name.levis.ichor.model.BgpPeer
import name.levis.ichor.model.BgpPeerReport
import name.levis.ichor.model.NetworkSection
import name.levis.ichor.model.NodeHealth
import name.levis.ichor.model.NodeNetwork
import name.levis.ichor.model.ShareTarget
import name.levis.ichor.model.isDown
import name.levis.ichor.model.showsBgp
import name.levis.ichor.model.sinceMillis
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class BgpAlertsTest {
    private val now = 1_800_000_000_000L
    private val node = "192.0.2.20"
    private val key = "$node|fabric|198.51.100.2"
    private val value = "warning|node-a|fabric|198.51.100.2|ACTIVE"
    private val down = BgpDown(node, "node-a", "fabric", "198.51.100.2", "ACTIVE")

    private fun report(vararg down: BgpDown, error: String = "") =
        BgpPeerReport("lab", listOf(BgpNodePeers(node, "node-a", supported = true, error = error)), down.toList())

    private fun snap(issues: Map<String, String>?, watched: Boolean = true) = ClusterSnapshot(
        context = "lab",
        takenAt = now,
        nodes = mapOf(node to NodeState("node-a", NodeHealth.READY)),
        etcdChecked = true,
        bgpWatched = watched,
        bgpChecked = watched && issues != null,
        bgpIssues = issues.orEmpty(),
    )

    @Test
    fun theGoJsonDecodes() {
        val json = """{"context":"lab","nodes":[{"node":"$node","hostname":"node-a","supported":true,"peers":[
            {"instance":"fabric","peer":"198.51.100.1","peerAsn":64512,"localAsn":64601,"state":"ESTABLISHED","established":true,
             "since":"2026-11-20T08:30:00Z","received":14,"advertised":2,"gracefulRestart":true,"peerRestarting":false},
            {"instance":"fabric","peer":"198.51.100.2","peerAsn":64513,"localAsn":64601,"state":"ACTIVE","established":false,
             "since":"","received":0,"advertised":0,"gracefulRestart":false,"peerRestarting":false}]}],
            "down":[{"node":"$node","hostname":"node-a","instance":"fabric","peer":"198.51.100.2","state":"ACTIVE"}]}"""
        val r = TalosJson.decodeFromString(BgpPeerReport.serializer(), json)
        assertEquals(mapOf(key to value), bgpIssuesOf(r))
        val up = r.nodes.single().peers.first()
        assertEquals(64512L, up.peerAsn)
        assertEquals(1_795_163_400_000L, up.sinceMillis)
        assertFalse(up.isDown)
        assertTrue(r.nodes.single().peers[1].isDown)
    }

    @Test
    fun aPeerRestartingUnderGracefulRestartIsNotDown() {
        assertFalse(BgpPeer(state = "IDLE", gracefulRestart = true, peerRestarting = true).isDown)
    }

    @Test
    fun aSessionDownOnceIsPendingThenAlertsAtTheNextCheck() {
        val first = evaluate(snap(emptyMap()), snap(bgpIssuesOf(report(down))), now)
        assertTrue(first.alerts.isEmpty())
        assertEquals(listOf(key), first.next.bgpPending)

        val second = evaluate(first.next, snap(bgpIssuesOf(report(down))), now)
        val alert = second.alerts.single()
        assertEquals(AlertKind.BGP_DOWN, alert.kind)
        assertEquals("bgp:$key", alert.key)
        assertEquals("198.51.100.2", alert.subject)
        assertEquals("bgp", alert.ruleKind)
        assertEquals(ShareTarget.network(node, "node-a"), alert.shareTarget())
        assertEquals(AlertChannel.NODES, alert.channel)
    }

    @Test
    fun aRebootBlipNeverNotifies() {
        // Down at one check (the node just rebooted), established at the next: nothing at all.
        val first = evaluate(snap(emptyMap()), snap(bgpIssuesOf(report(down))), now)
        val second = evaluate(first.next, snap(bgpIssuesOf(report())), now)
        assertTrue(second.alerts.isEmpty())
        assertTrue(second.next.bgpPending.isEmpty())
    }

    @Test
    fun anUnreachableNodeKeepsWhatWasKnown() {
        val known = mapOf(key to value)
        assertEquals(known, bgpIssuesOf(report(error = "node unreachable"), known))
        // Another node's issue is not kept by this node's error.
        assertTrue(bgpIssuesOf(report(error = "x"), mapOf("192.0.2.99|fabric|p" to value)).isEmpty())
    }

    @Test
    fun aSessionBackUpSaysSoOnce() {
        val notified = snap(mapOf(key to value))
        val back = evaluate(notified, snap(bgpIssuesOf(report())), now)
        val alert = back.alerts.single()
        assertEquals(AlertKind.BGP_UP, alert.kind)
        assertFalse(alert.problem)
    }

    @Test
    fun aSeparatorInTheTextNeverShiftsTheFields() {
        val odd = down.copy(instance = "a|b", peer = "link/eth1")
        val d = BgpDetail.parse(bgpIssuesOf(report(odd)).values.single())
        assertEquals("a/b", d.instance)
        assertEquals("link/eth1", d.peer)
        assertEquals("ACTIVE", d.state)
    }

    @Test
    fun theNetworkScreenShowsBgpOnlyWhenTheNodePeers() {
        val peer = BgpPeer(peer = "198.51.100.1", state = "ESTABLISHED", established = true)
        assertTrue(NodeNetwork(bgpSupported = true, bgpPeers = listOf(peer)).showsBgp)
        assertFalse(NodeNetwork(bgpSupported = true).showsBgp)
        assertFalse(NodeNetwork(bgpSupported = false, bgpPeers = listOf(peer)).showsBgp)
        assertTrue(NodeNetwork(bgpSupported = true, errors = mapOf(NetworkSection.BGP to "refused")).showsBgp)
    }
}
