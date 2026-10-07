package name.levis.ichor.model

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertSame
import org.junit.Test

class TopologyLastKnownTest {

    private val workerUp = TopologyNode(
        id = "worker-1", node = "10.1.0.10", hostname = "worker-1", role = "worker", addresses = listOf("10.1.0.10"),
        zone = "fr-1", country = "FR", site = "site-1", queried = true,
    )
    private val peer = TopologyNode(id = "peer", node = "10.0.0.2", hostname = "peer", zone = "dk-1", country = "DK", site = "site-2", queried = true)

    // What the core says of a target that does not answer: named by its address, alone.
    private val workerDown = TopologyNode(
        id = "10.1.0.10", node = "10.1.0.10", hostname = "10.1.0.10", addresses = listOf("10.1.0.10"),
        site = "site-9", queried = true, error = "no route to host",
    )

    private val before = ClusterTopology(
        nodes = listOf(workerUp, peer),
        sites = listOf(
            TopologySite("site-1", "fr-1", "zone", "FR", listOf("worker-1")),
            TopologySite("site-2", "dk-1", "zone", "DK", listOf("peer")),
        ),
    )

    @Test
    fun anUnreachableNodeKeepsItsNameRoleAndZone() {
        val now = ClusterTopology(
            nodes = listOf(peer, workerDown),
            sites = listOf(TopologySite("site-1", "dk-1", "zone", "DK", listOf("peer")), TopologySite("site-9", kind = "node", nodes = listOf("10.1.0.10"))),
        )
        val node = now.withLastKnown(before, previousAt = 1_000).nodes.single { it.id == "10.1.0.10" }
        assertEquals("worker-1", node.hostname)
        assertEquals("worker", node.role)
        assertEquals("fr-1", node.zone)
        assertEquals("FR", node.country)
        assertEquals("no route to host", node.error)
        assertEquals(1_000L, node.lastSeen)
    }

    @Test
    fun itGoesBackToTheSiteOfItsZone() {
        val neighbour = TopologyNode(id = "neighbour", node = "10.0.0.3", hostname = "neighbour", zone = "fr-1", site = "site-1", queried = true)
        val now = ClusterTopology(
            nodes = listOf(neighbour, workerDown),
            sites = listOf(TopologySite("site-1", "fr-1", "zone", "FR", listOf("neighbour")), TopologySite("site-9", kind = "node", nodes = listOf("10.1.0.10"))),
        )
        val merged = now.withLastKnown(before, 1_000)
        assertEquals(listOf(TopologySite("site-1", "fr-1", "zone", "FR", listOf("neighbour", "10.1.0.10"))), merged.sites)
        assertEquals("site-1", merged.nodes.single { it.id == "10.1.0.10" }.site)
    }

    @Test
    fun aLoneNodeHasItsSiteNamedAsBefore() {
        val now = ClusterTopology(nodes = listOf(workerDown), sites = listOf(TopologySite("site-9", kind = "node", nodes = listOf("10.1.0.10"))))
        assertEquals(listOf(TopologySite("site-9", "fr-1", "zone", "FR", listOf("10.1.0.10"))), now.withLastKnown(before, 1_000).sites)
    }

    @Test
    fun aNodeStillDownKeepsWhenItWasLastSeen() {
        val now = ClusterTopology(nodes = listOf(workerDown), sites = listOf(TopologySite("site-9", kind = "node", nodes = listOf("10.1.0.10"))))
        val first = now.withLastKnown(before, 1_000)
        val second = now.withLastKnown(first, 2_000).nodes.single()
        assertEquals("worker-1", second.hostname)
        assertEquals(1_000L, second.lastSeen)
    }

    @Test
    fun aNodeNeverSeenUpStaysNamedByItsAddress() {
        val now = ClusterTopology(nodes = listOf(workerDown))
        val node = now.withLastKnown(ClusterTopology(nodes = listOf(workerDown)), 1_000).nodes.single()
        assertEquals("10.1.0.10", node.hostname)
        assertNull(node.lastSeen)
    }

    @Test
    fun aNameAnotherNodeGoesByIsNotReused() {
        val otherNamesake = peer.copy(id = "worker-1", hostname = "worker-1")
        val now = ClusterTopology(nodes = listOf(otherNamesake, workerDown))
        assertSame(now.nodes[1], now.withLastKnown(before, 1_000).nodes[1])
    }

    @Test
    fun reachableNodesAndNoPreviousMapAreLeftAsTheyAre() {
        val now = ClusterTopology(nodes = listOf(workerUp.copy(hostname = "renamed")))
        assertSame(now, now.withLastKnown(null, 1_000))
        assertEquals(now, now.withLastKnown(before, 1_000))
    }
}
