package name.levis.ichor.model

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test

class LastKnownTest {

    private fun up(addr: String, hostname: String = "host-$addr") = NodeOverview(
        node = addr, hostname = hostname, reachable = true, version = "v1.11.0", arch = "amd64",
        platform = "metal", role = "controlplane", stage = "running", ready = true,
        cpuCount = 8, memTotal = 32, memAvailable = 16,
    )

    private fun down(addr: String) =
        NodeOverview(node = addr, hostname = addr, reachable = false, error = "no route to host", errorKind = "network")

    private fun overview(vararg nodes: NodeOverview) = ClusterOverview("ctx", nodes.toList())

    @Test
    fun anUnreachableNodeKeepsWhatItWasWhenItLastAnswered() {
        val merged = overview(down("10.0.0.1")).withLastKnown(overview(up("10.0.0.1")), previousAt = 1_000)
        val node = merged.nodes.single()
        assertEquals("host-10.0.0.1", node.hostname)
        assertEquals("controlplane", node.role)
        assertEquals("v1.11.0", node.version)
        assertEquals("amd64", node.arch)
        assertEquals("metal", node.platform)
        assertEquals(8, node.cpuCount)
        assertEquals(32L, node.memTotal)
        assertEquals(1_000L, node.lastSeen)
    }

    @Test
    fun itsHealthAndErrorStayTheNewOnes() {
        val node = overview(down("a")).withLastKnown(overview(up("a")), 1_000).nodes.single()
        assertFalse(node.reachable)
        assertFalse(node.ready)
        assertEquals("unknown", node.stage)
        assertEquals("no route to host", node.error)
        assertEquals("network", node.errorKind)
        assertEquals(0L, node.memAvailable)
    }

    @Test
    fun aNodeStillDownKeepsWhenItWasLastSeen() {
        val first = overview(down("a")).withLastKnown(overview(up("a")), previousAt = 1_000)
        val second = overview(down("a")).withLastKnown(first, previousAt = 2_000)
        assertEquals(1_000L, second.nodes.single().lastSeen)
        assertEquals("host-a", second.nodes.single().hostname)
    }

    @Test
    fun aNodeThatAnswersAgainIsLeftAsItIs() {
        val first = overview(down("a")).withLastKnown(overview(up("a")), 1_000)
        val fresh = overview(up("a", hostname = "renamed"))
        assertEquals(fresh, fresh.withLastKnown(first, 2_000))
        assertNull(fresh.withLastKnown(first, 2_000).nodes.single().lastSeen)
    }

    @Test
    fun aNodeNeverSeenAnsweringStaysAsTheCoreSaid() {
        val node = overview(down("a")).withLastKnown(overview(down("a")), 1_000).nodes.single()
        assertEquals("a", node.hostname)
        assertNull(node.lastSeen)
    }

    @Test
    fun nodesAreMatchedByAddress() {
        val merged = overview(down("a"), down("b")).withLastKnown(overview(up("b")), 1_000)
        assertNull(merged.nodes[0].lastSeen)
        assertEquals("host-b", merged.nodes[1].hostname)
    }

    @Test
    fun withoutAPreviousOverviewNothingChanges() {
        val fresh = overview(down("a"))
        assertSame(fresh, fresh.withLastKnown(null, 0))
    }

    @Test
    fun hasLastKnownOnlyOnceANodeWasFilledIn() {
        assertFalse(overview(down("a")).hasLastKnown)
        assertTrue(overview(down("a")).withLastKnown(overview(up("a")), 1_000).hasLastKnown)
    }
}
