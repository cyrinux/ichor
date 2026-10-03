package name.levis.ichor.model

import kotlinx.serialization.Serializable

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

/** An IPv4 address of the phone on a local network, with its prefix length. */
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
 * The IPv4 networks to scan for Talos nodes, in order and within [MAX_SCAN_HOSTS]: the
 * phone's own networks ([local], at most a /22 around its address), the /24 of each
 * private address the talosconfig lists ([known] endpoints and nodes, a port allowed), then
 * [COMMON_PRIVATE_NETWORKS]. Only private ranges (RFC 1918 and CGNAT): never the Internet.
 */
fun scanNetworks(local: List<LocalAddress>, known: List<String>): List<String> {
    val own = local.mapNotNull { a ->
        val ip = parseIpv4(a.address)?.takeIf(::isPrivate) ?: return@mapNotNull null
        network(ip, a.prefixLength.coerceIn(WIDEST_LOCAL_PREFIX, 32))
    }
    val listed = known.mapNotNull { parseIpv4(hostOf(it))?.takeIf(::isPrivate) }.map { network(it, NEIGHBOUR_PREFIX) }
    val common = COMMON_PRIVATE_NETWORKS.map { cidr ->
        val (ip, bits) = cidr.split('/')
        network(parseIpv4(ip)!!, bits.toInt())
    }

    // A network inside one already listed adds nothing.
    return (own + listed + common).fold(emptyList<Pair<Long, Int>>()) { picked, net ->
        val covered = picked.any { (base, bits) -> bits <= net.second && network(net.first, bits).first == base }
        val hosts = picked.sumOf { hostCount(it.second) } + hostCount(net.second)
        if (covered || hosts > MAX_SCAN_HOSTS) picked else picked + net
    }.map { (base, bits) -> "${formatIpv4(base)}/$bits" }
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

private fun hostCount(bits: Int): Long = 1L shl (32 - bits)

private fun network(ip: Long, bits: Int): Pair<Long, Int> {
    val mask = if (bits == 0) 0L else (0xFFFFFFFFL shl (32 - bits)) and 0xFFFFFFFFL
    return (ip and mask) to bits
}

private fun isPrivate(ip: Long): Boolean =
    inRange(ip, "10.0.0.0", 8) || inRange(ip, "172.16.0.0", 12) || inRange(ip, "192.168.0.0", 16) || inRange(ip, "100.64.0.0", 10)

private fun inRange(ip: Long, base: String, bits: Int): Boolean = network(ip, bits).first == parseIpv4(base)

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
