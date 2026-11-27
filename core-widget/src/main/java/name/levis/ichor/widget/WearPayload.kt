package name.levis.ichor.widget

import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json

/** The Data Layer path of the phone's snapshot for the watch. */
const val WEAR_SNAPSHOT_PATH = "/snapshot"

/** The DataMap key holding [WearPayload] as JSON bytes. */
const val WEAR_PAYLOAD_KEY = "payload"

/** At most this many nodes per cluster go to the watch (the ones down first). */
const val WEAR_MAX_NODES = 30

/**
 * What the phone sends the watch after each background check: per cluster its label (already
 * masked in screenshot mode), node counts and its first [WEAR_MAX_NODES] nodes (a short host
 * name, none in screenshot mode, and a state). No address, endpoint, error text or credential.
 */
@Serializable
data class WearPayload(
    /** The cluster on screen on the phone first. */
    val clusters: List<WearCluster> = emptyList(),
    /** The whole fleet's tally (null from an older phone app, or before the first check). */
    val fleet: FleetTally? = null,
)

@Serializable
data class WearCluster(
    val label: String,
    val takenAt: Long,
    val counts: NodeCounts,
    /** The nodes, the ones down first (empty from an older phone app). */
    val nodes: List<WearNode> = emptyList(),
)

/** A node's state on the watch. */
enum class WearNodeHealth { READY, NOT_READY, UNREACHABLE }

/** Why a node does not answer, as a category: never the error's text (it names the endpoint). */
enum class WearNodeProblem { TIMEOUT, REFUSED, NO_SUCH_HOST, TLS, UNREACHABLE }

/** A node on the watch: its short host name (empty when masked or an address) and its state. */
@Serializable
data class WearNode(
    val name: String = "",
    val health: WearNodeHealth,
    /** Set for an unreachable node only. */
    val problem: WearNodeProblem? = null,
)

/** The category of a node's read error (Go's wording), as the Apple Watch words it too. */
fun wearNodeProblem(error: String): WearNodeProblem {
    val text = error.lowercase()
    return when {
        "no such host" in text -> WearNodeProblem.NO_SUCH_HOST
        "connection refused" in text -> WearNodeProblem.REFUSED
        listOf("timeout", "timed out", "deadline exceeded").any { it in text } -> WearNodeProblem.TIMEOUT
        listOf("tls", "x509", "certificate").any { it in text } -> WearNodeProblem.TLS
        else -> WearNodeProblem.UNREACHABLE
    }
}

private val ADDRESS = Regex("""^[0-9.]+$|:|^\[""")

/**
 * The name a node shows on the watch: the first label of its host name ("cp-1" of
 * "cp-1.lab.example"); empty in screenshot mode or when the host name is an address.
 */
fun wearNodeName(hostname: String, masked: Boolean): String {
    val host = hostname.trim()
    if (masked || host.isEmpty() || ADDRESS.containsMatchIn(host)) return ""
    return host.substringBefore('.')
}

/** [nodes] for the watch: unreachable first, then not ready, then by name; at most [max]. */
fun wearNodes(nodes: List<WearNode>, max: Int = WEAR_MAX_NODES): List<WearNode> =
    nodes.sortedWith(compareBy<WearNode> { it.health.rank() }.thenBy { it.name }).take(max)

/** Under the Data Layer's 100 KB item limit, with room to spare. */
const val WEAR_PAYLOAD_LIMIT = 90 * 1024

/**
 * [p] within [limit] bytes of JSON, giving up the least useful first: the ready nodes of the
 * clusters after the first (the one on screen), then all their nodes. Their counts stay.
 */
fun trimmedWearPayload(p: WearPayload, limit: Int = WEAR_PAYLOAD_LIMIT): WearPayload {
    val fits = { q: WearPayload -> encodeWearPayload(q).size <= limit }
    if (fits(p)) return p
    val keep = listOf<(WearNode) -> Boolean>({ it.health != WearNodeHealth.READY }, { false })
    var trimmed = p
    for (rule in keep) {
        trimmed = trimmed.copy(clusters = trimmed.clusters.mapIndexed { i, c -> if (i == 0) c else c.copy(nodes = c.nodes.filter(rule)) })
        if (fits(trimmed)) return trimmed
    }
    return trimmed
}

private fun WearNodeHealth.rank(): Int = when (this) {
    WearNodeHealth.UNREACHABLE -> 0
    WearNodeHealth.NOT_READY -> 1
    WearNodeHealth.READY -> 2
}

private val WearJson = Json { ignoreUnknownKeys = true }

fun encodeWearPayload(p: WearPayload): ByteArray = WearJson.encodeToString(WearPayload.serializer(), p).encodeToByteArray()

/** Null for bytes of an unknown shape (a newer or older phone app): the watch shows no data. */
fun decodeWearPayload(bytes: ByteArray): WearPayload? =
    runCatching { WearJson.decodeFromString(WearPayload.serializer(), bytes.decodeToString()) }.getOrNull()
