package name.levis.ichor.model

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import java.util.Locale

// Mirrors go/talosmobile/kube_netperf.go.

const val NETPERF_PATH_POD = "pod"
const val NETPERF_PATH_HOST = "host"
const val NETPERF_THROUGHPUT = "throughput"
const val NETPERF_LATENCY = "latency"

const val NETPERF_PHASE_PREPARING = "preparing"
const val NETPERF_PHASE_STARTING = "starting"
const val NETPERF_PHASE_TESTING = "testing"
const val NETPERF_PHASE_CLEANING = "cleaning"

/** Seconds each measurement can last, offered in the setup. */
val NETPERF_DURATIONS = listOf(5, 10, 20)
const val NETPERF_DEFAULT_SECONDS = 10

@Serializable
data class NetPerfNodeList(val nodes: List<NetPerfNode> = emptyList())

@Serializable
data class NetPerfNode(
    val name: String,
    val address: String = "",
    val controlPlane: Boolean = false,
    val ready: Boolean = false,
)

/** Round trip in microseconds. */
@Serializable
data class NetPerfLatency(
    val min: Double = 0.0,
    val mean: Double = 0.0,
    val max: Double = 0.0,
    val p50: Double = 0.0,
    val p90: Double = 0.0,
    val p99: Double = 0.0,
)

@Serializable
data class NetPerfResult(
    /** [NETPERF_PATH_POD] or [NETPERF_PATH_HOST]. */
    val path: String,
    /** [NETPERF_THROUGHPUT] or [NETPERF_LATENCY]. */
    val test: String,
    val throughputMbps: Double = 0.0,
    /** Round trips per second. */
    val transactionRate: Double = 0.0,
    @SerialName("latencyUs") val latency: NetPerfLatency? = null,
    val error: String = "",
)

@Serializable
data class NetPerfReport(
    val server: String = "",
    val client: String = "",
    val hostNetwork: Boolean = false,
    val seconds: Int = 0,
    val image: String = "",
    /** Unix millis. */
    val started: Long = 0,
    val finished: Long = 0,
    val results: List<NetPerfResult> = emptyList(),
)

@Serializable
data class NetPerfProgress(
    val phase: String,
    val path: String = "",
    val test: String = "",
    /** 1-based measurement number while [phase] is [NETPERF_PHASE_TESTING]. */
    val step: Int = 0,
    val steps: Int = 0,
    /** While starting: "node: reason" of a pod waiting, e.g. its image being pulled. */
    val message: String = "",
    val at: Long = 0,
    val results: List<NetPerfResult> = emptyList(),
)

/** What the user chose to test. */
data class NetPerfSetup(
    val server: String = "",
    val client: String = "",
    val hostNetwork: Boolean = false,
    val seconds: Int = NETPERF_DEFAULT_SECONDS,
) {
    val ready: Boolean get() = server.isNotEmpty() && client.isNotEmpty()

    /** Measurements the test makes: throughput and latency per network path. */
    val steps: Int get() = if (hostNetwork) 4 else 2
}

/**
 * The pair a test starts with: two ready workers when there are, else two ready nodes, else
 * the one ready node against itself. Null without a ready node.
 */
fun defaultNetPerfPair(nodes: List<NetPerfNode>): Pair<String, String>? {
    val ready = nodes.filter { it.ready }
    val workers = ready.filterNot { it.controlPlane }
    val pick = if (workers.size >= 2) workers else ready
    return when {
        pick.size >= 2 -> pick[0].name to pick[1].name
        pick.size == 1 -> pick[0].name to pick[0].name
        else -> null
    }
}

/** [setup] with nodes that exist and are ready, the default pair filling what is not. */
fun NetPerfSetup.withNodes(nodes: List<NetPerfNode>): NetPerfSetup {
    val ready = nodes.filter { it.ready }.map { it.name }.toSet()
    val pair = defaultNetPerfPair(nodes) ?: return copy(server = "", client = "")
    return copy(
        server = server.takeIf { it in ready } ?: pair.first,
        client = client.takeIf { it in ready } ?: pair.second,
    )
}

/** Finished tests kept per cluster on the phone. */
const val NETPERF_HISTORY_LIMIT = 20

/** What was tested, to show a saved report like the test that made it. */
val NetPerfReport.setup: NetPerfSetup get() = NetPerfSetup(server, client, hostNetwork, seconds)

/**
 * These saved tests, newest first, with [report] added in front and at most [limit] kept; a
 * test already there (same start) is replaced.
 */
fun List<NetPerfReport>.withReport(report: NetPerfReport, limit: Int = NETPERF_HISTORY_LIMIT): List<NetPerfReport> =
    (listOf(report) + filterNot { it.started == report.started }).take(limit)

/** One saved test in a pair's trend: its pod-to-pod figures, null where not measured. */
data class NetPerfTrendPoint(val started: Long, val throughputMbps: Double?, val p50Us: Double?)

/** The saved tests from [client] to [server], oldest first, for the trend charts. */
fun List<NetPerfReport>.trendOf(client: String, server: String): List<NetPerfTrendPoint> =
    filter { it.client == client && it.server == server }
        .sortedBy { it.started }
        .map { report ->
            val pod = report.results.filter { it.path == NETPERF_PATH_POD && it.error.isEmpty() }
            NetPerfTrendPoint(
                started = report.started,
                throughputMbps = pod.firstOrNull { it.test == NETPERF_THROUGHPUT }?.throughputMbps,
                p50Us = pod.firstOrNull { it.test == NETPERF_LATENCY }?.latency?.p50,
            )
        }

/**
 * The newest saved test between nodes [a] and [b], either way round, that measured pod-to-pod
 * throughput: what the KubeSpan map shows on their link. Null when they were never tested.
 */
fun List<NetPerfReport>.latestBetween(a: String, b: String): NetPerfReport? =
    filter { (it.client == a && it.server == b) || (it.client == b && it.server == a) }
        .filter { it.podThroughputMbps != null }
        .maxByOrNull { it.started }

/** Pod-to-pod throughput of this test, null when not measured. */
val NetPerfReport.podThroughputMbps: Double?
    get() = results.firstOrNull { it.path == NETPERF_PATH_POD && it.test == NETPERF_THROUGHPUT && it.error.isEmpty() }?.throughputMbps

/** "9.41 Gbit/s", "870 Mbit/s". */
fun formatMbps(mbps: Double): String = when {
    mbps >= 1000 -> String.format(Locale.ROOT, "%.2f Gbit/s", mbps / 1000)
    mbps >= 10 -> String.format(Locale.ROOT, "%.0f Mbit/s", mbps)
    else -> String.format(Locale.ROOT, "%.1f Mbit/s", mbps)
}

/** "58 µs", "1.87 ms". */
fun formatMicros(us: Double): String = when {
    us >= 1000 -> String.format(Locale.ROOT, "%.2f ms", us / 1000)
    else -> String.format(Locale.ROOT, "%.0f µs", us)
}
