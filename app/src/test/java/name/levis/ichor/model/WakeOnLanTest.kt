package name.levis.ichor.model

import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class WakeOnLanTest {

    private val mac = byteArrayOf(0xaa.toByte(), 0xbb.toByte(), 0xcc.toByte(), 0x01, 0x02, 0x03)

    @Test
    fun macsParseInTheUsualNotations() {
        listOf("aa:bb:cc:01:02:03", "AA-BB-CC-01-02-03", "aabb.cc01.0203", "aabbcc010203", " aa:bb:cc:01:02:03 ")
            .forEach { assertArrayEquals(it, mac, parseMac(it)) }
    }

    @Test
    fun notAMac() {
        listOf("", "aa:bb:cc:01:02", "aa:bb:cc:01:02:03:04", "gg:bb:cc:01:02:03", "10.0.0.1").forEach { assertNull(it, parseMac(it)) }
    }

    @Test
    fun macsAreWrittenLowercaseWithColons() {
        assertEquals("aa:bb:cc:01:02:03", formatMac(mac))
    }

    @Test
    fun magicPacketIsSyncThenSixteenMacs() {
        val packet = magicPacket(mac)
        assertEquals(102, packet.size)
        assertTrue(packet.take(6).all { it == 0xff.toByte() })
        (0 until 16).forEach { i -> assertArrayEquals(mac, packet.copyOfRange(6 + i * 6, 12 + i * 6)) }
    }

    @Test
    fun broadcastIsOptionalIpv4OrHostName() {
        listOf("", "  ", "192.168.1.255", "255.255.255.255", "wol-relay.lan", "router").forEach { assertTrue(it, isWolAddress(it)) }
        listOf("192.168.1.256", "192.168.1", "1.2.3.4.5", "bad host", "-x.lan", "a|b").forEach { assertFalse(it, isWolAddress(it)) }
    }

    @Test
    fun directedBroadcastOfTheSubnet() {
        val ip = byteArrayOf(192.toByte(), 168.toByte(), 1, 20)
        assertEquals("192.168.1.255", directedBroadcast(ip, 24))
        assertEquals("192.168.1.23", directedBroadcast(ip, 30))
        assertEquals("255.255.255.255", directedBroadcast(ip, 0))
        assertEquals("192.168.1.20", directedBroadcast(ip, 32))
        assertNull(directedBroadcast(ip, 33))
        assertNull(directedBroadcast(ByteArray(16), 64))
    }

    @Test
    fun typedFieldsBecomeATarget() {
        assertEquals(
            WolTarget("aa:bb:cc:01:02:03", "192.168.1.255", 7),
            parseWolTarget("AA-BB-CC-01-02-03", " 192.168.1.255 ", "7").getOrThrow(),
        )
        val defaults = parseWolTarget("aabbcc010203", "", "").getOrThrow()
        assertEquals(WOL_DEFAULT_PORT, defaults.port)
        assertEquals(WOL_DEFAULT_BROADCAST, defaults.address)
    }

    @Test
    fun badFieldsSayWhichOne() {
        fun error(mac: String, broadcast: String, port: String) =
            (parseWolTarget(mac, broadcast, port).exceptionOrNull() as WolInputException).error
        assertEquals(WolInputError.MAC, error("nope", "", ""))
        assertEquals(WolInputError.BROADCAST, error("aabbcc010203", "300.1.1.1", ""))
        assertEquals(WolInputError.PORT, error("aabbcc010203", "", "0"))
        assertEquals(WolInputError.PORT, error("aabbcc010203", "", "70000"))
    }

    @Test
    fun storedFormRoundTrips() {
        val target = WolTarget("aa:bb:cc:01:02:03", "10.0.0.255", 7)
        assertEquals(target, decodeWolTarget(encodeWolTarget(target)))
        assertEquals(WolTarget("aa:bb:cc:01:02:03"), decodeWolTarget(encodeWolTarget(WolTarget("aa:bb:cc:01:02:03"))))
        listOf("", "aa:bb:cc:01:02:03", "nope||9", "aabbcc010203||x", "aabbcc010203||0").forEach { assertNull(it, decodeWolTarget(it)) }
    }

    @Test
    fun nodesOfRemovedClustersAreForgotten() {
        val target = WolTarget("aa:bb:cc:01:02:03")
        val saved = mapOf(wolKey("fp-prod", "10.0.0.1") to target, wolKey("fp-old", "10.0.0.2") to target)
        assertEquals(mapOf(wolKey("fp-prod", "10.0.0.1") to target), keepWolTargets(saved, listOf("fp-prod", "")))
    }

    @Test
    fun candidatesArePhysicalEthernetLinks() {
        val links = listOf(
            LinkInfo("eth0", type = "ether", hardwareAddr = "aa:bb:cc:01:02:03"),
            LinkInfo("eth1", type = "ether", hardwareAddr = "aa:bb:cc:01:02:04"),
            LinkInfo("bond0", type = "ether", kind = "bond", hardwareAddr = "aa:bb:cc:01:02:03"),
            LinkInfo("lxc1", type = "ether", hardwareAddr = "aa:bb:cc:01:02:05", virtual = true),
            LinkInfo("lo", type = "loopback", hardwareAddr = "00:00:00:00:00:00"),
            LinkInfo("wg0", type = "none", kind = "wireguard"),
        )
        assertEquals(listOf("eth0", "eth1"), wolCandidates(links).map { it.name })
    }
}
