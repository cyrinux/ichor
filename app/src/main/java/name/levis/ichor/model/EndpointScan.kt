package name.levis.ichor.model

import kotlinx.serialization.Serializable
import java.math.BigInteger

/** A host found on the network that answered the Talos API with the credentials of [contexts]. */
@Serializable
data class EndpointMatch(
    val endpoint: String,
    val hostname: String = "",
    val version: String = "",
    /** Machine type: controlplane, init or worker ("" when unknown). */
    val role: String = "",
    val contexts: List<String> = emptyList(),
)

/** What an endpoint said about itself when tested with a context's credentials. */
@Serializable
data class EndpointProbe(
    val endpoint: String,
    val hostname: String = "",
    val version: String = "",
    val role: String = "",
)

/** An IPv4 or IPv6 address of the phone on a local network, with its prefix length. */
data class LocalAddress(val address: String, val prefixLength: Int)

/** Hosts a scan covers at most, as the Go side enforces: a few seconds on a phone. */
const val MAX_SCAN_HOSTS = 4096

/** The phone's own network is scanned up to this size (a /22) around its address. */
private const val WIDEST_LOCAL_PREFIX = 22
private const val NEIGHBOUR_PREFIX = 24

/**
 * The /24s home and lab routers hand out by default, tried after the phone's own network
 * and those of the talosconfig: a node shared from elsewhere is usually on one of them.
 * The private ranges as a whole (10/8 alone is 16 million hosts) are far too large to sweep.
 */
val COMMON_PRIVATE_NETWORKS = listOf(
    "192.168.0.0/24",
    "192.168.1.0/24",
    "192.168.2.0/24",
    "192.168.178.0/24",
    "10.0.0.0/24",
    "10.0.1.0/24",
    "172.16.0.0/24",
)

/**
 * An IPv6 network is swept as a /120 (256 hosts): a /64 holds far too many addresses. The
 * Go side takes up to a /116.
 */
private const val IPV6_SCAN_PREFIX = 120

/**
 * The networks to scan for Talos nodes, in order and within [MAX_SCAN_HOSTS]: the phone's
 * own networks ([local]: at most a /22 around an IPv4 address; the first /120 of an IPv6
 * prefix, where statically numbered nodes sit, SLAAC addresses being random), the
 * neighbourhood of each private address the talosconfig lists ([known] endpoints and nodes,
 * a port allowed: its IPv4 /24 or IPv6 /120), then [COMMON_PRIVATE_NETWORKS]. Only private
 * ranges (RFC 1918, CGNAT, IPv6 unique local fc00::/7): never the Internet.
 */
fun scanNetworks(local: List<LocalAddress>, known: List<String>): List<String> {
    val own = local.mapNotNull { a ->
        val ip = parseIp(a.address)?.takeIf(::isPrivate) ?: return@mapNotNull null
        if (ip.width == IPV4_BITS) {
            network(ip, a.prefixLength.coerceIn(WIDEST_LOCAL_PREFIX, IPV4_BITS))
        } else {
            network(network(ip, a.prefixLength.coerceIn(0, IPV6_SCAN_PREFIX)).base, IPV6_SCAN_PREFIX)
        }
    }
    val listed = known.mapNotNull { parseIp(hostOf(it))?.takeIf(::isPrivate) }
        .map { network(it, if (it.width == IPV4_BITS) NEIGHBOUR_PREFIX else IPV6_SCAN_PREFIX) }
    val common = COMMON_PRIVATE_NETWORKS.map { cidr ->
        val (ip, bits) = cidr.split('/')
        network(parseIp(ip)!!, bits.toInt())
    }

    // A network inside one already listed adds nothing.
    return (own + listed + common).fold(emptyList<IpNetwork>()) { picked, net ->
        val covered = picked.any { it.contains(net) }
        val hosts = picked.sumOf { it.hostCount } + net.hostCount
        if (covered || hosts > MAX_SCAN_HOSTS) picked else picked + net
    }.map { it.toString() }
}

/** The address or hostname of an endpoint, without its port (a bare IPv6 address left as is). */
fun hostOf(endpoint: String): String {
    if (endpoint.startsWith("[")) return endpoint.substringBefore("]").removePrefix("[")
    val colon = endpoint.indexOf(':')
    return if (colon > 0 && endpoint.indexOf(':', colon + 1) < 0) endpoint.substring(0, colon) else endpoint
}

/**
 * Whether [text] can be one endpoint of a list: not blank, no separator. The Go side checks
 * the rest (address or hostname, optional port) when testing or saving, and says what is wrong.
 */
fun isEndpoint(text: String): Boolean =
    text.isNotEmpty() && text.length <= ENDPOINT_MAX && text.none { it.isWhitespace() || it == ',' }

const val ENDPOINT_MAX = 260

private const val IPV4_BITS = 32
private const val IPV6_BITS = 128
private const val IPV6_GROUPS = 8
private const val GROUP_BITS = 16
private const val GROUP_MASK = 0xFFFF

/** An IPv4 (32 bits wide) or IPv6 (128 bits) address. */
private data class IpAddress(val value: BigInteger, val width: Int)

/** The network of [base] (host bits zero) with a [bits]-long prefix. */
private data class IpNetwork(val base: IpAddress, val bits: Int) {
    /** Only small networks are built here (a /22 or a /120 at most), so this fits. */
    val hostCount: Long get() = 1L shl (base.width - bits)

    fun contains(other: IpNetwork): Boolean =
        base.width == other.base.width && bits <= other.bits && network(other.base, bits).base == base

    override fun toString(): String =
        "${if (base.width == IPV4_BITS) formatIpv4(base.value.toLong()) else formatIpv6(base.value)}/$bits"
}

private fun network(ip: IpAddress, bits: Int): IpNetwork {
    val hostBits = ip.width - bits
    return IpNetwork(ip.copy(value = ip.value.shiftRight(hostBits).shiftLeft(hostBits)), bits)
}

private val PRIVATE_NETWORKS = listOf("10.0.0.0" to 8, "172.16.0.0" to 12, "192.168.0.0" to 16, "100.64.0.0" to 10, "fc00::" to 7)
    .map { (base, bits) -> network(parseIp(base)!!, bits) }

private fun isPrivate(ip: IpAddress): Boolean = PRIVATE_NETWORKS.any { it.contains(network(ip, ip.width)) }

/** A literal address (an IPv6 one without a zone: a link-local address is not swept), or null. */
private fun parseIp(text: String): IpAddress? =
    parseIpv4(text)?.let { IpAddress(BigInteger.valueOf(it), IPV4_BITS) }
        ?: parseIpv6(text)?.let { IpAddress(it, IPV6_BITS) }

private fun parseIpv6(text: String): BigInteger? {
    val halves = text.split("::")
    if (':' !in text || halves.size > 2) return null
    // An embedded IPv4 address ends the address: never before "::".
    if (halves.size == 2 && halves[1].isNotEmpty() && '.' in halves[0]) return null
    val head = ipv6Groups(halves[0]) ?: return null
    val tail = if (halves.size == 2) ipv6Groups(halves[1]) ?: return null else emptyList()
    val missing = IPV6_GROUPS - head.size - tail.size
    if (if (halves.size == 1) missing != 0 else missing < 1) return null
    return (head + List(missing) { 0 } + tail).fold(BigInteger.ZERO) { acc, group ->
        acc.shiftLeft(GROUP_BITS).or(BigInteger.valueOf(group.toLong()))
    }
}

/** The 16-bit groups of one side of "::" ("" has none), a trailing dotted IPv4 counting as two. */
private fun ipv6Groups(part: String): List<Int>? {
    if (part.isEmpty()) return emptyList()
    val fields = part.split(':')
    return fields.flatMapIndexed { i, field ->
        if (i == fields.lastIndex && '.' in field) {
            val v4 = parseIpv4(field) ?: return null
            listOf((v4 shr GROUP_BITS).toInt(), (v4 and GROUP_MASK.toLong()).toInt())
        } else {
            if (field.isEmpty() || field.length > 4 || !field.all { it.isDigit() || it.lowercaseChar() in 'a'..'f' }) return null
            listOf(field.toInt(16))
        }
    }
}

/** RFC 5952 text, as Go writes it: lower case, the first longest run of zero groups as "::". */
private fun formatIpv6(ip: BigInteger): String {
    val groups = (IPV6_GROUPS - 1 downTo 0).map { ip.shiftRight(it * GROUP_BITS).toInt() and GROUP_MASK }
    val hex = groups.map { it.toString(16) }
    val zeroRuns = groups.indices.filter { groups[it] == 0 && (it == 0 || groups[it - 1] != 0) }
        .map { start -> start to groups.drop(start).takeWhile { it == 0 }.size }
    val (start, length) = zeroRuns.maxByOrNull { it.second }?.takeIf { it.second >= 2 }
        ?: return hex.joinToString(":")
    return hex.take(start).joinToString(":") + "::" + hex.drop(start + length).joinToString(":")
}

private fun parseIpv4(text: String): Long? {
    val parts = text.split('.')
    if (parts.size != 4) return null
    return parts.fold(0L) { acc, part ->
        val octet = part.takeIf { it.isNotEmpty() && it.length <= 3 && it.all(Char::isDigit) }?.toInt() ?: return null
        if (octet > 255) return null
        (acc shl 8) or octet.toLong()
    }
}

private fun formatIpv4(ip: Long): String = (3 downTo 0).joinToString(".") { ((ip shr (it * 8)) and 0xFF).toString() }
