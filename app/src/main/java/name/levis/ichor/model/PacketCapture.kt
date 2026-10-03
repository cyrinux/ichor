package name.levis.ichor.model

import kotlinx.serialization.Serializable
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale
import java.util.TimeZone

// Mirrors go/ichorgo/pcap*.go.

/**
 * One captured packet as summarized by the Go core; [n] is its index in the pcap file, from 0.
 * Live summaries are throttled by the core, so [n] may skip packets that are only in the file.
 */
@Serializable
data class PacketSummary(
    val n: Long,
    /** Unix millis. */
    val ts: Long = 0,
    val len: Long = 0,
    val src: String = "",
    val dst: String = "",
    val proto: String = "",
    val info: String = "",
)

/** A page of a pcap file read back ([total] packets in the whole file). */
@Serializable
data class PcapPage(val packets: List<PacketSummary> = emptyList(), val total: Long = 0)

@Serializable
data class PacketField(val k: String, val v: String = "")

@Serializable
data class PacketLayer(val name: String, val fields: List<PacketField> = emptyList())

/** A decoded packet: its layers (Ethernet, IPv4, TCP, DNS…) and the raw bytes as a hex dump. */
@Serializable
data class PacketDetail(val layers: List<PacketLayer> = emptyList(), val hex: String = "")

/** Packets read per page when a saved capture is opened. */
const val PCAP_PAGE_SIZE = 500

/** Most summaries kept on screen during a live capture; the file keeps them all. */
const val MAX_LIVE_PACKETS = 5000

/** Bytes kept per packet; 0 keeps whole packets. */
const val CAPTURE_SNAP_LEN = 0L

/** Capture length choices, in seconds; the default is 30 s. */
val CAPTURE_DURATIONS = listOf(10L, 30L, 60L, 300L)
const val DEFAULT_CAPTURE_SECONDS = 30L

private const val MIB = 1024L * 1024L

/** Capture size limits, in bytes; the default is 20 MiB. */
val CAPTURE_MAX_SIZES = listOf(5 * MIB, 20 * MIB, 100 * MIB)
const val DEFAULT_CAPTURE_BYTES = 20 * MIB

/** Capture filter presets offered as chips (labels are translated in the UI). */
enum class FilterPreset(val expression: String) {
    DNS("udp port 53"),
    ICMP("icmp or icmp6"),
    HTTPS("tcp port 443"),
    KUBERNETES_API("tcp port 6443"),
    // No Talos API preset: the core always excludes port 50000 (the capture's own stream).
}

data class CaptureOptions(
    val iface: String = "",
    val filter: String = "",
    val promiscuous: Boolean = false,
    val maxSeconds: Long = DEFAULT_CAPTURE_SECONDS,
    val maxBytes: Long = DEFAULT_CAPTURE_BYTES,
)

/** Why the capture cannot start yet. */
enum class CaptureProblem { NO_INTERFACE, BAD_FILTER, BAD_DURATION, BAD_SIZE }

/**
 * What prevents [options] from starting, or null when they are fine. [filterError] is the
 * Go core's validation of the filter ("" when valid).
 */
fun CaptureOptions.problem(filterError: String): CaptureProblem? = when {
    iface.isBlank() -> CaptureProblem.NO_INTERFACE
    filter.isNotBlank() && filterError.isNotEmpty() -> CaptureProblem.BAD_FILTER
    maxSeconds <= 0 -> CaptureProblem.BAD_DURATION
    maxBytes <= 0 -> CaptureProblem.BAD_SIZE
    else -> null
}

/**
 * Interfaces offered for a capture: physical links before virtual ones, up before down, then
 * by name. Virtual (CNI/pod) links only when [showVirtual].
 */
fun captureInterfaces(links: List<LinkInfo>, showVirtual: Boolean): List<LinkInfo> =
    links.filter { showVirtual || !it.virtual }
        .sortedWith(compareBy<LinkInfo>({ it.virtual }, { !it.isUp }, { it.type == "loopback" }, { it.name }))

/** Default interface: the first physical link that is up, else the first offered one. */
fun defaultCaptureInterface(links: List<LinkInfo>): String? =
    captureInterfaces(links, showVirtual = false).firstOrNull()?.name ?: links.firstOrNull()?.name

/** `<host>-<iface>-<yyyyMMdd-HHmmss>.pcap`, with unsafe file name characters replaced. */
fun captureFileName(hostname: String, iface: String, now: Date = Date(), zone: TimeZone = TimeZone.getDefault()): String {
    val stamp = SimpleDateFormat("yyyyMMdd-HHmmss", Locale.ROOT).apply { timeZone = zone }.format(now)
    return "${safeFilePart(hostname)}-${safeFilePart(iface)}-$stamp.pcap"
}

private fun safeFilePart(part: String) = part.replace(Regex("[^A-Za-z0-9._-]+"), "-").trim('-', '.').ifEmpty { "unknown" }

/** Whether [name] looks like a capture this app wrote (no path separators, .pcap). */
fun isCaptureFileName(name: String): Boolean =
    name.endsWith(".pcap") && '/' !in name && !name.startsWith(".") && name.length > ".pcap".length

/** Colour family of a protocol tag in the packet list. */
enum class ProtoKind { TCP, UDP, ICMP, DNS, ARP, TLS, OTHER }

fun protoKind(proto: String): ProtoKind = when (proto.uppercase(Locale.ROOT)) {
    "TCP" -> ProtoKind.TCP
    "UDP", "NTP", "DHCP", "DHCPV6" -> ProtoKind.UDP
    "ICMP", "ICMPV6" -> ProtoKind.ICMP
    "DNS" -> ProtoKind.DNS
    "ARP" -> ProtoKind.ARP
    "TLS", "HTTP" -> ProtoKind.TLS
    else -> ProtoKind.OTHER // IPv4/IPv6 without a known transport, tunnels (WireGuard, VXLAN, Geneve)
}

/** Time since the first packet, "+12.345". Negative offsets (clock skew) show as +0.000. */
fun relativeTime(ts: Long, start: Long): String {
    val ms = (ts - start).coerceAtLeast(0)
    return String.format(Locale.ROOT, "+%d.%03d", ms / 1000, ms % 1000)
}

/** "00:42", "05:00": elapsed capture time. */
fun formatElapsed(seconds: Long): String {
    val s = seconds.coerceAtLeast(0)
    return String.format(Locale.ROOT, "%02d:%02d", s / 60, s % 60)
}
