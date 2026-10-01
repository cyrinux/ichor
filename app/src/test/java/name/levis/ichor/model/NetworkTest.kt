package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class NetworkTest {

    private val network = NodeNetwork(
        links = listOf(LinkInfo("eth0", state = "up"), LinkInfo("lxc1234", kind = "veth", virtual = true)),
        addresses = listOf(AddressInfo("10.0.0.2/24", link = "eth0"), AddressInfo("10.244.0.1/32", link = "lxc1234", virtual = true)),
        routes = listOf(
            RouteInfo("default", gateway = "10.0.0.1", link = "eth0"),
            RouteInfo("default", link = "cilium_host", virtual = true),
            RouteInfo("10.244.0.5/32", link = "lxc1234", virtual = true),
            RouteInfo("10.0.0.0/24", link = "eth0"),
        ),
    )

    @Test
    fun virtualItemsAreHiddenButDefaultRoutesKept() {
        val shown = network.visible(showVirtual = false)
        assertEquals(listOf("eth0"), shown.links.map { it.name })
        assertEquals(listOf("10.0.0.2/24"), shown.addresses.map { it.address })
        assertEquals(listOf("default", "default", "10.0.0.0/24"), shown.routes.map { it.destination })
        assertEquals(3, network.hiddenVirtualCount())
        assertEquals(network, network.visible(showVirtual = true))
    }

    @Test
    fun defaultRouteAndLinkState() {
        assertTrue(RouteInfo("default").isDefault)
        assertFalse(RouteInfo("0.0.0.0/1").isDefault)
        assertTrue(LinkInfo("eth0", state = "UP").isUp)
        assertFalse(LinkInfo("eth1", state = "down").isUp)
    }

    private val sockets = listOf(
        ConnectionInfo("tcp", "0.0.0.0", 50000, state = "LISTEN", listening = true, pid = 1, processName = "apid"),
        ConnectionInfo("udp", "0.0.0.0", 123, listening = true, processName = "timed"),
        ConnectionInfo("tcp", "10.0.0.2", 50000, "10.0.0.9", 41000, state = "ESTABLISHED", pid = 1, processName = "apid"),
    )

    @Test
    fun connectionsFilterAndSearch() {
        assertEquals(2, sockets.filtered(ConnectionFilter.LISTENING, "").size)
        assertEquals(3, sockets.filtered(ConnectionFilter.ALL, " ").size)
        assertEquals(2, sockets.filtered(ConnectionFilter.ALL, "APID").size)
        assertEquals(1, sockets.filtered(ConnectionFilter.ALL, "10.0.0.9").size)
        assertEquals(1, sockets.filtered(ConnectionFilter.ALL, "10.0.0.9:41000").size)
        assertEquals(1, sockets.filtered(ConnectionFilter.LISTENING, "123").size)
        assertEquals(0, sockets.filtered(ConnectionFilter.LISTENING, "established").size)
    }

    @Test
    fun endpointFormatting() {
        assertEquals("*:50000", endpoint("0.0.0.0", 50000))
        assertEquals("*:53", endpoint("::", 53))
        assertEquals("10.0.0.1:443", endpoint("10.0.0.1", 443))
        assertEquals("[fd00::1]:443", endpoint("fd00::1", 443))
    }

    @Test
    fun decodesGoJson() {
        val json = """
            {"links":[{"name":"eth0","type":"ether","kind":"","state":"up","hardwareAddr":"aa:bb","mtu":1500,"speedMbit":1000,"virtual":false}],
             "addresses":[],"routes":[{"destination":"default","gateway":"10.0.0.1","link":"eth0","metric":1024,"table":"main","family":"inet4","virtual":false}],
             "resolvers":["1.1.1.1"],"timeServers":["time.cloudflare.com"],"errors":{"addresses":"permission denied"}}
        """.trimIndent()
        val n = TalosJson.decodeFromString(NodeNetwork.serializer(), json)
        assertEquals(1500L, n.links.single().mtu)
        assertEquals("permission denied", n.errors[NetworkSection.ADDRESSES])
        val c = TalosJson.decodeFromString(ConnectionInfo.serializer(), """{"protocol":"tcp","localIp":"::","localPort":22,"remoteIp":"","remotePort":0,"state":"LISTEN","listening":true}""")
        assertEquals(0L, c.pid)
        assertEquals("", c.processName)
    }
}
