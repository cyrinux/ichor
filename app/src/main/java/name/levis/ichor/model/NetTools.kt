package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/nettools.go and nettools_parse.go (StartNodeNetTool).

/** The checks a node can run on its network; [wire] is StartNodeNetTool's tool. */
enum class NetTool(val wire: String) {
    DNS("dns"),
    PING("ping"),
    PORT("port"),
    TRACE("trace"),
    HTTP("http"),
}

@Serializable
data class NetDnsRecord(val name: String = "", val type: String = "", val ttl: Int = 0, val value: String = "")

@Serializable
data class NetDnsResult(
    /** The answer's status (NOERROR, NXDOMAIN…), "" when dig got none. */
    val status: String = "",
    val records: List<NetDnsRecord> = emptyList(),
    val server: String = "",
    val queryMs: Int = 0,
)

@Serializable
data class NetPingResult(
    val sent: Int = 0,
    val received: Int = 0,
    val lossPct: Double = 0.0,
    val minMs: Double = 0.0,
    val avgMs: Double = 0.0,
    val maxMs: Double = 0.0,
)

@Serializable
data class NetPortResult(val open: Boolean = false, val message: String = "")

/** A hop of the path; [host] is "???" when it did not answer. */
@Serializable
data class NetTraceHop(val hop: Int = 0, val host: String = "", val lossPct: Double = 0.0, val avgMs: Double = 0.0) {
    val silent: Boolean get() = host == "???"
}

@Serializable
data class NetHttpResult(
    /** The final status after redirects; 0: no answer. */
    val status: Int = 0,
    val redirectUrl: String = "",
    val totalMs: Double = 0.0,
    /** An https URL; [tlsOk] then says whether its certificate verified. */
    val tls: Boolean = false,
    val tlsOk: Boolean = false,
    val subject: String = "",
    val issuer: String = "",
    val notBefore: String = "",
    val notAfter: String = "",
    val daysLeft: Int = 0,
    /** curl's, when it could not connect. */
    val error: String? = null,
)

/** A tool's parsed run: only the tool's own section is set; [raw] is the full output. */
@Serializable
data class NetToolResult(
    val tool: String = "",
    val target: String = "",
    val ok: Boolean = false,
    val exitCode: Int = 0,
    val dns: NetDnsResult? = null,
    val ping: NetPingResult? = null,
    val port: NetPortResult? = null,
    val trace: List<NetTraceHop>? = null,
    val http: NetHttpResult? = null,
    val raw: String = "",
)

/** The options JSON of StartNodeNetTool; zero or empty fields are left to the core's defaults. */
@Serializable
data class NetToolOptions(val record: String = "", val server: String = "", val count: Int = 0, val timeoutSec: Int = 0)

sealed interface NetToolEvent {
    /** A progress or output line, as it comes. */
    data class Line(val text: String) : NetToolEvent

    data class Done(val result: NetToolResult) : NetToolEvent

    data class Failed(val message: String) : NetToolEvent
}

/** The DNS record types offered (the core's list). */
val NET_DNS_RECORDS = listOf("A", "AAAA", "CNAME", "MX", "NS", "PTR", "SRV", "TXT")

/** The characters no target may hold (the core refuses them too). */
private val NET_TARGET_FORBIDDEN = " \t\r\n;|&$`<>(){}'\"\\*!".toSet()

/**
 * Why [target] cannot be given to [tool], checked in the field before running, or null when it
 * looks right. The core checks it again: this only saves a round trip.
 */
fun netTargetProblem(tool: NetTool, target: String): NetTargetProblem? {
    val t = target.trim()
    return when {
        t.isEmpty() -> NetTargetProblem.EMPTY
        t.startsWith("-") || t.any { it in NET_TARGET_FORBIDDEN } -> NetTargetProblem.CHARACTERS
        tool == NetTool.PORT && !Regex("^(\\[[0-9a-fA-F:.]+]|[^:\\[\\]]+):[0-9]{1,5}$").matches(t) -> NetTargetProblem.HOST_PORT
        tool == NetTool.HTTP && !(t.startsWith("http://") || t.startsWith("https://")) -> NetTargetProblem.URL
        tool != NetTool.HTTP && tool != NetTool.PORT && t.contains('/') -> NetTargetProblem.HOST
        else -> null
    }
}

enum class NetTargetProblem { EMPTY, CHARACTERS, HOST_PORT, URL, HOST }

/** The last targets used, newest first, at most [max], [target] moved to the front. */
fun rememberTarget(recent: List<String>, target: String, max: Int = 10): List<String> =
    (listOf(target.trim()) + recent.filter { it != target.trim() }).filter { it.isNotEmpty() }.take(max)
