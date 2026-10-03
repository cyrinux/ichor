package name.levis.ichor.model

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class EndpointScanTest {

    @Test
    fun ownNetworkFirstThenListedThenCommon() {
        val networks = scanNetworks(
            local = listOf(LocalAddress("192.168.42.17", 24)),
            known = listOf("10.20.30.40", "10.20.30.41:50001", "203.0.113.5", "talos.example.com", "fd00::1"),
        )
        assertEquals(
            listOf("192.168.42.0/24", "10.20.30.0/24") + COMMON_PRIVATE_NETWORKS,
            networks,
        )
    }

    @Test
    fun wideLocalNetworkIsNarrowedAroundThePhone() {
        val networks = scanNetworks(listOf(LocalAddress("10.5.130.9", 16)), emptyList())
        assertEquals("10.5.128.0/22", networks.first())
    }

    @Test
    fun publicAndCoveredNetworksAreSkipped() {
        val networks = scanNetworks(
            local = listOf(LocalAddress("192.168.0.0", 23), LocalAddress("8.8.8.8", 24)),
            known = listOf("192.168.1.7"),
        )
        // 192.168.0.0/24 and 192.168.1.0/24 are inside the phone's /23.
        assertEquals("192.168.0.0/23", networks.first())
        assertFalse(networks.any { it.startsWith("8.8.") })
        assertFalse("192.168.1.0/24" in networks)
        assertFalse("192.168.0.0/24" in networks)
    }

    @Test
    fun scanStaysWithinTheHostLimit() {
        val local = (0 until 8).map { LocalAddress("10.$it.0.1", 22) }
        val networks = scanNetworks(local, emptyList())
        val hosts = networks.sumOf { 1 shl (32 - it.substringAfter('/').toInt()) }
        assertTrue(hosts <= MAX_SCAN_HOSTS)
        assertEquals(4, networks.size)
    }

    @Test
    fun cgnatCountsAsPrivate() {
        assertEquals("100.64.3.0/24", scanNetworks(listOf(LocalAddress("100.64.3.9", 24)), emptyList()).first())
    }

    @Test
    fun hostOfDropsThePort() {
        assertEquals("10.0.0.1", hostOf("10.0.0.1:50000"))
        assertEquals("10.0.0.1", hostOf("10.0.0.1"))
        assertEquals("fd00::1", hostOf("fd00::1"))
        assertEquals("node.lan", hostOf("node.lan:50001"))
    }

    @Test
    fun localAddresses() {
        listOf("10.1.2.3", "172.20.0.1", "192.168.1.1", "100.64.0.1", "169.254.3.4", "fd00::1", "fe80::1", "NAS.local")
            .forEach { assertTrue(it, isLocalAddress(it)) }
        listOf("8.8.8.8", "172.32.0.1", "2001:db8::1", "talos.example.com", "localnet")
            .forEach { assertFalse(it, isLocalAddress(it)) }
        assertEquals("fd00::1", hostOf("[fd00::1]:50000"))
    }

    @Test
    fun clusterOnLocalNetwork() {
        assertTrue(ContextSummary("lan", endpoints = listOf("talos.example.com"), nodes = listOf("192.168.1.10")).onLocalNetwork())
        assertTrue(ContextSummary("lan", endpoints = listOf("10.0.0.1:50000")).onLocalNetwork())
        assertFalse(ContextSummary("remote", endpoints = listOf("203.0.113.5", "talos.example.com")).onLocalNetwork())
    }

    @Test
    fun endpointRejectsSeparators() {
        assertTrue(isEndpoint("10.0.0.1:50000"))
        assertTrue(isEndpoint("node-1.lan"))
        assertFalse(isEndpoint(""))
        assertFalse(isEndpoint("10.0.0.1, 10.0.0.2"))
        assertFalse(isEndpoint("a b"))
    }
}
