package name.levis.ichor.model

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class PublicIpTest {

    private fun node(addr: String, hostname: String, publicIPs: List<String> = emptyList(), reachable: Boolean = true) =
        NodeOverview(node = addr, hostname = hostname, reachable = reachable, publicIPs = publicIPs)

    private val report = PublicIpReport(
        listOf(
            PublicIpProbe(name = "cp-1", address = "10.0.0.2", publicIP = "203.0.113.2"),
            PublicIpProbe(name = "edge-1", address = "172.16.0.9", publicIP = "198.51.100.9"),
            PublicIpProbe(name = "lan-1", address = "10.0.0.4", error = "no answer"),
        ),
        at = 1,
    )

    @Test
    fun matchesByAddressThenByHostname() {
        assertEquals("203.0.113.2", report.ipFor(node("10.0.0.2", "other-name")))
        assertEquals("198.51.100.9", report.ipFor(node("192.0.2.9", "edge-1.example.org")))
        assertNull(report.ipFor(node("10.0.0.4", "lan-1")))
        // An unreachable node's hostname is its address: no name to match.
        assertNull(report.ipFor(node("10.9.9.9", "10.9.9.9")))
    }

    @Test
    fun talosKnowledgeWinsOverAProbe() {
        assertEquals(listOf("2001:db8::2"), node("10.0.0.2", "cp-1", listOf("2001:db8::2")).shownPublicIps(report))
        assertEquals(listOf("203.0.113.2"), node("10.0.0.2", "cp-1").shownPublicIps(report))
        assertEquals(emptyList<String>(), node("10.0.0.2", "cp-1").shownPublicIps(null))
    }

    @Test
    fun lacksPublicIpsOnlyCountsNodesThatAnswer() {
        val known = node("10.0.0.2", "cp-1", listOf("203.0.113.2"))
        assertFalse(ClusterOverview("c", listOf(known, node("10.0.0.3", "10.0.0.3", reachable = false))).lacksPublicIps())
        assertTrue(ClusterOverview("c", listOf(known, node("10.0.0.3", "w-1"))).lacksPublicIps())
    }

    @Test
    fun firstErrorNamesTheNode() {
        assertEquals("lan-1: no answer", report.firstError())
        assertEquals("", PublicIpReport().firstError())
    }
}
