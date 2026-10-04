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
            listOf("192.168.42.0/24", "10.20.30.0/24", "fd00::/120") + COMMON_PRIVATE_NETWORKS,
            networks,
        )
    }

    @Test
    fun ownUlaNetworkIsSweptFromTheStartOfItsPrefix() {
        // SLAAC addresses are random: statically numbered nodes sit low in the /64.
        val networks = scanNetworks(
            local = listOf(
                LocalAddress("192.168.42.17", 24),
                LocalAddress("fd12:3456:789a:1:abcd:ef01:2345:6789", 64),
                LocalAddress("fd12:3456:789a:2::1:5", 120),
            ),
            known = emptyList(),
        )
        assertEquals(listOf("192.168.42.0/24", "fd12:3456:789a:1::/120", "fd12:3456:789a:2::1:0/120"), networks.take(3))
    }

    @Test
    fun globalAndLinkLocalIpv6AreSkipped() {
        val networks = scanNetworks(
            local = listOf(LocalAddress("2001:db8::5", 64), LocalAddress("fe80::1", 64)),
            known = listOf("2001:db8::7", "fe80::2%wlan0", "[2001:db8::8]:50000"),
        )
        assertEquals(COMMON_PRIVATE_NETWORKS, networks)
    }

    @Test
    fun listedIpv6EndpointsGiveTheirNeighbourhood() {
        val networks = scanNetworks(
            local = listOf(LocalAddress("fd00:0:0:1::99", 64)),
            known = listOf("[fd00:0:0:1::7]:50001", "FD00:0:0:2::1:20", "[fd00::3:4]", "fd00::1.2.3.4"),
        )
        // fd00:0:0:1::7 is inside the phone's own /120 already; the first longest zero run is the one shortened.
        assertEquals(listOf("fd00:0:0:1::/120", "fd00::2:0:0:1:0/120", "fd00::3:0/120", "fd00::102:300/120"), networks.take(4))
        assertEquals(4 + COMMON_PRIVATE_NETWORKS.size, networks.size)
    }

    @Test
    fun malformedIpv6IsIgnored() {
        val known = listOf("fd00:::1", "fd00::1::2", "fd00:12345::1", "fd00:g::1", "1:2:3:4:5:6:7:8:9", "fd00:1:2:3:4:5:6", "fd00::1.2.3.4:5")
        assertEquals(COMMON_PRIVATE_NETWORKS, scanNetworks(emptyList(), known))
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
    fun hostOfUnwrapsBracketedIpv6() {
        assertEquals("fd00::1", hostOf("[fd00::1]:50000"))
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
