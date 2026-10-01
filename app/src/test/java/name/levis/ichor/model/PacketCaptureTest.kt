package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.Date
import java.util.TimeZone

class PacketCaptureTest {

    @Test
    fun decodesSummary() {
        val p = TalosJson.decodeFromString(
            PacketSummary.serializer(),
            """{"n":3,"ts":1790000000123,"len":74,"src":"192.0.2.1:53","dst":"192.0.2.2:40000","proto":"DNS","info":"A example.com","extra":1}""",
        )
        assertEquals(PacketSummary(3, 1_790_000_000_123, 74, "192.0.2.1:53", "192.0.2.2:40000", "DNS", "A example.com"), p)
    }

    @Test
    fun decodesPageAndDetailWithNulls() {
        val page = TalosJson.decodeFromString(PcapPage.serializer(), """{"packets":null,"total":0}""")
        assertTrue(page.packets.isEmpty())
        val d = TalosJson.decodeFromString(
            PacketDetail.serializer(),
            """{"layers":[{"name":"IPv4","fields":[{"k":"TTL","v":"64"}]},{"name":"Payload","fields":null}],"hex":"0000  45 00"}""",
        )
        assertEquals(listOf("IPv4", "Payload"), d.layers.map { it.name })
        assertEquals(PacketField("TTL", "64"), d.layers[0].fields.single())
        assertTrue(d.layers[1].fields.isEmpty())
    }

    @Test
    fun optionsProblems() {
        val ok = CaptureOptions(iface = "eth0")
        assertNull(ok.problem(""))
        assertEquals(CaptureProblem.NO_INTERFACE, CaptureOptions().problem(""))
        assertEquals(CaptureProblem.BAD_FILTER, ok.copy(filter = "tcp port").problem("syntax error"))
        // An empty filter captures everything, whatever a stale validation said.
        assertNull(ok.copy(filter = "  ").problem("syntax error"))
        assertEquals(CaptureProblem.BAD_DURATION, ok.copy(maxSeconds = 0).problem(""))
        assertEquals(CaptureProblem.BAD_SIZE, ok.copy(maxBytes = -1).problem(""))
    }

    @Test
    fun defaultsAreAmongChoices() {
        assertTrue(DEFAULT_CAPTURE_SECONDS in CAPTURE_DURATIONS)
        assertTrue(DEFAULT_CAPTURE_BYTES in CAPTURE_MAX_SIZES)
        assertEquals(listOf(10L, 30L, 60L, 300L), CAPTURE_DURATIONS)
        assertEquals(listOf(5L, 20L, 100L).map { it * 1024 * 1024 }, CAPTURE_MAX_SIZES)
    }

    @Test
    fun interfacesPhysicalAndUpFirst() {
        val links = listOf(
            LinkInfo("lxc123", kind = "veth", state = "up", virtual = true),
            LinkInfo("eth1", type = "ether", state = "down"),
            LinkInfo("lo", type = "loopback", state = "up"),
            LinkInfo("eth0", type = "ether", state = "up"),
            LinkInfo("bond0", type = "ether", kind = "bond", state = "up"),
        )
        assertEquals(listOf("bond0", "eth0", "lo", "eth1"), captureInterfaces(links, showVirtual = false).map { it.name })
        assertEquals(listOf("bond0", "eth0", "lo", "eth1", "lxc123"), captureInterfaces(links, showVirtual = true).map { it.name })
        assertEquals("bond0", defaultCaptureInterface(links))
        assertEquals("lxc1", defaultCaptureInterface(listOf(LinkInfo("lxc1", virtual = true))))
        assertNull(defaultCaptureInterface(emptyList()))
    }

    @Test
    fun fileName() {
        val at = Date(1_790_000_000_000) // 2026-09-21T14:13:20Z
        assertEquals("cp-1-eth0-20260921-141320.pcap", captureFileName("cp-1", "eth0", at, TimeZone.getTimeZone("UTC")))
        assertEquals("node-a.lan-eth0.100-20260921-161320.pcap", captureFileName("node a.lan", "eth0.100", at, TimeZone.getTimeZone("Europe/Paris")))
        assertEquals("unknown-unknown-20260921-141320.pcap", captureFileName("../", "", at, TimeZone.getTimeZone("UTC")))
        assertTrue(isCaptureFileName(captureFileName("••••", "eth0", at)))
    }

    @Test
    fun captureFileNames() {
        assertTrue(isCaptureFileName("cp-1-eth0-20260921-141320.pcap"))
        assertFalse(isCaptureFileName("cp.pcap.part"))
        assertFalse(isCaptureFileName("../secret.pcap"))
        assertFalse(isCaptureFileName(".pcap"))
        assertFalse(isCaptureFileName("talosconfig.enc"))
    }

    @Test
    fun protoKinds() {
        assertEquals(ProtoKind.TCP, protoKind("TCP"))
        assertEquals(ProtoKind.UDP, protoKind("NTP"))
        assertEquals(ProtoKind.ICMP, protoKind("ICMPv6"))
        assertEquals(ProtoKind.DNS, protoKind("DNS"))
        assertEquals(ProtoKind.ARP, protoKind("ARP"))
        assertEquals(ProtoKind.TLS, protoKind("HTTP"))
        assertEquals(ProtoKind.OTHER, protoKind("WireGuard"))
        assertEquals(ProtoKind.OTHER, protoKind(""))
    }

    @Test
    fun times() {
        assertEquals("+0.000", relativeTime(1000, 1000))
        assertEquals("+12.345", relativeTime(13_345, 1000))
        assertEquals("+0.000", relativeTime(900, 1000))
        assertEquals("00:00", formatElapsed(-1))
        assertEquals("00:42", formatElapsed(42))
        assertEquals("05:00", formatElapsed(300))
    }
}
