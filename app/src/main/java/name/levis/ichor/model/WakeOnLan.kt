package name.levis.ichor.model

/** Where magic packets go when no broadcast address is given: the phone's own network. */
const val WOL_DEFAULT_BROADCAST = "255.255.255.255"

/** The discard port, the usual one for Wake-on-LAN (7 and 9 are both common). */
const val WOL_DEFAULT_PORT = 9

/**
 * How to wake one node: its MAC address and, optionally, where to send the magic packet
 * (a subnet's broadcast address, or a host that relays it) and on which UDP port.
 * Kept on this device only, like cluster names.
 */
data class WolTarget(
    /** Normalized, e.g. "aa:bb:cc:dd:ee:ff". */
    val mac: String,
    /** Blank: [WOL_DEFAULT_BROADCAST]. */
    val broadcast: String = "",
    val port: Int = WOL_DEFAULT_PORT,
) {
    val address: String get() = broadcast.ifBlank { WOL_DEFAULT_BROADCAST }
}

/** What is wrong with a typed Wake-on-LAN setting. */
enum class WolInputError { MAC, BROADCAST, PORT }

private val MAC_SEPARATORS = Regex("[:\\-. ]")
private val HOST_LABEL = Regex("[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?")

/**
 * The six bytes of [input], written with ':' or '-' separators, Cisco-style dots, or none
 * ("aa:bb:cc:dd:ee:ff", "AA-BB-CC-DD-EE-FF", "aabb.ccdd.eeff", "aabbccddeeff"); null if
 * it is not a MAC address.
 */
fun parseMac(input: String): ByteArray? {
    val hex = input.trim().replace(MAC_SEPARATORS, "")
    if (hex.length != 12 || !hex.all { it.isDigit() || it.lowercaseChar() in 'a'..'f' }) return null
    return ByteArray(6) { i -> hex.substring(i * 2, i * 2 + 2).toInt(16).toByte() }
}

/** [mac] as "aa:bb:cc:dd:ee:ff". */
fun formatMac(mac: ByteArray): String = mac.joinToString(":") { "%02x".format(it.toInt() and 0xff) }

/** 6 × 0xFF, then the MAC 16 times: what a network card listens for to power its machine on. */
fun magicPacket(mac: ByteArray): ByteArray {
    require(mac.size == 6) { "a MAC address has 6 bytes" }
    return ByteArray(6) { 0xff.toByte() } + ByteArray(16 * 6) { i -> mac[i % 6] }
}

/** The broadcast address of the IPv4 subnet [address]/[prefix], e.g. 192.168.1.20/24 → 192.168.1.255. */
fun directedBroadcast(address: ByteArray, prefix: Int): String? {
    if (address.size != 4 || prefix !in 0..32) return null
    val ip = address.fold(0L) { acc, b -> (acc shl 8) or (b.toLong() and 0xff) }
    val host = (1L shl (32 - prefix)) - 1
    val broadcast = ip or host
    return (3 downTo 0).joinToString(".") { ((broadcast shr (it * 8)) and 0xff).toString() }
}

/** An IPv4 address or a host name to send to; blank is fine (the default broadcast). */
fun isWolAddress(input: String): Boolean {
    val text = input.trim()
    if (text.isEmpty()) return true
    if (text.length > 253) return false
    val parts = text.split('.')
    if (parts.all { part -> part.isNotEmpty() && part.all(Char::isDigit) }) {
        return parts.size == 4 && parts.all { it.length <= 3 && it.toInt() <= 255 }
    }
    return parts.all { HOST_LABEL.matches(it) }
}

/** The typed fields as a [WolTarget], or what is wrong with them. Blank port: [WOL_DEFAULT_PORT]. */
fun parseWolTarget(mac: String, broadcast: String, port: String): Result<WolTarget> {
    val bytes = parseMac(mac) ?: return Result.failure(WolInputException(WolInputError.MAC))
    if (!isWolAddress(broadcast)) return Result.failure(WolInputException(WolInputError.BROADCAST))
    val portNumber = if (port.isBlank()) WOL_DEFAULT_PORT else port.trim().toIntOrNull()
    if (portNumber == null || portNumber !in 1..65535) return Result.failure(WolInputException(WolInputError.PORT))
    return Result.success(WolTarget(formatMac(bytes), broadcast.trim(), portNumber))
}

class WolInputException(val error: WolInputError) : IllegalArgumentException(error.name)

/** The store key of [node] (its talosconfig address) in the cluster [fingerprint]. */
fun wolKey(fingerprint: String, node: String): String = "$fingerprint|$node"

/** How a [WolTarget] is stored: "mac|broadcast|port" (none of them can hold a '|'). */
fun encodeWolTarget(target: WolTarget): String = "${target.mac}|${target.broadcast}|${target.port}"

/** The [WolTarget] [encodeWolTarget] stored as [value]; null if it is not one. */
fun decodeWolTarget(value: String): WolTarget? {
    val parts = value.split('|')
    if (parts.size != 3) return null
    val bytes = parseMac(parts[0]) ?: return null
    val port = parts[2].toIntOrNull()?.takeIf { it in 1..65535 } ?: return null
    return WolTarget(formatMac(bytes), parts[1], port)
}

/** The settings of [saved] (by [wolKey]) whose cluster is still among [fingerprints]. */
fun keepWolTargets(saved: Map<String, WolTarget>, fingerprints: List<String>): Map<String, WolTarget> {
    val known = fingerprints.filter { it.isNotBlank() }.toSet()
    return saved.filterKeys { it.substringBefore('|') in known }
}

/** The MAC addresses a node reports for its physical Ethernet links, to pick one from. */
fun wolCandidates(links: List<LinkInfo>): List<LinkInfo> =
    links.filter { !it.virtual && it.kind.isEmpty() && it.type == "ether" && parseMac(it.hardwareAddr) != null }
        .distinctBy { parseMac(it.hardwareAddr)!!.toList() }
