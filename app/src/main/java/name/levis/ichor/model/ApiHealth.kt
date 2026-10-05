package name.levis.ichor.model

import kotlinx.serialization.Serializable
import java.util.Locale

// Mirrors go/ichorgo/kube_apihealth.go.

/** The overall verdict of [ApiHealthReport.status], worst first. */
enum class ApiStatus { UNHEALTHY, THROTTLING, BUSY, OK }

/**
 * The Kubernetes API server's health and what puts pressure on it. Rates are per second over
 * [windowSeconds], or since the server started when it is 0 (two servers answered).
 */
@Serializable
data class ApiHealthReport(
    val status: String = "",
    val version: String = "",
    val ready: ApiProbe = ApiProbe(),
    val live: ApiProbe = ApiProbe(),
    val uptimeSeconds: Long = 0,
    val windowSeconds: Double = 0.0,
    val requestRate: Double = 0.0,
    /** 5xx answers. */
    val errorRate: Double = 0.0,
    /** 429 answers. */
    val throttledRate: Double = 0.0,
    /** Refused by API Priority and Fairness. */
    val rejectedRate: Double = 0.0,
    val inflightRead: Int = 0,
    val inflightMutate: Int = 0,
    val queued: Int = 0,
    val watches: Int = 0,
    val watchEventRate: Double = 0.0,
    val etcdLatencyMs: Double = 0.0,
    val clients: List<ApiFlow> = emptyList(),
    val priorities: List<ApiPriority> = emptyList(),
    val requests: List<ApiRequestRow> = emptyList(),
    val watchedKinds: List<ApiCount> = emptyList(),
    val objects: List<ApiCount> = emptyList(),
    val queuedRequests: List<ApiQueued> = emptyList(),
    /** /metrics could not be read: only the probes are known. */
    val metricsError: String = "",
)

/** A /readyz or /livez answer; [error] when the probe itself got none. */
@Serializable
data class ApiProbe(val ok: Boolean = false, val checks: List<ApiCheck> = emptyList(), val error: String = "")

@Serializable
data class ApiCheck(val name: String, val ok: Boolean = false, val reason: String = "")

/** A flow schema: API Priority and Fairness's group of clients (nodes, controllers, service accounts…). */
@Serializable
data class ApiFlow(
    val name: String,
    val priority: String = "",
    val rate: Double = 0.0,
    val rejectedRate: Double = 0.0,
    val queued: Int = 0,
    /** Mean time queued before running. */
    val waitMs: Double = 0.0,
)

/** A priority level: the seats its requests use out of its [limit] (0 for exempt). */
@Serializable
data class ApiPriority(
    val name: String,
    val executing: Double = 0.0,
    val limit: Double = 0.0,
    val queued: Int = 0,
    val rejectedRate: Double = 0.0,
)

/** One verb on one resource; an empty [resource] is a non-resource path such as /healthz. */
@Serializable
data class ApiRequestRow(
    val verb: String,
    val resource: String = "",
    val rate: Double = 0.0,
    val errorRate: Double = 0.0,
    val latencyMs: Double = 0.0,
)

@Serializable
data class ApiCount(val resource: String, val count: Int = 0)

/** A request waiting in an API Priority and Fairness queue now, with who sent it. */
@Serializable
data class ApiQueued(
    val user: String = "",
    val flowSchema: String = "",
    val priority: String = "",
    val verb: String = "",
    val path: String = "",
)

val ApiHealthReport.verdict: ApiStatus
    get() = when (status) {
        "unhealthy" -> ApiStatus.UNHEALTHY
        "throttling" -> ApiStatus.THROTTLING
        "busy" -> ApiStatus.BUSY
        else -> ApiStatus.OK
    }

/** The checks that failed, readyz first, each once. */
val ApiHealthReport.failedChecks: List<ApiCheck>
    get() = (ready.checks + live.checks).filter { !it.ok }.distinctBy { it.name }

/** Whether the rates cover the last seconds, not the server's whole life. */
val ApiHealthReport.ratesAreLive: Boolean get() = windowSeconds > 0

/** The share of the level's seats in use, null for exempt (no limit). */
val ApiPriority.share: Float? get() = if (limit > 0) (executing / limit).toFloat().coerceIn(0f, 1f) else null

/** Past this share of its seats, a priority level counts as nearly full (as in Go). */
const val API_PRIORITY_FULL = 0.8f

/** A rate as "142/s", "3.6/s" or "0.02/s": two significant figures below 10. */
fun formatRate(perSecond: Double): String = when {
    perSecond >= 10 -> String.format(Locale.ROOT, "%.0f/s", perSecond)
    perSecond >= 1 -> String.format(Locale.ROOT, "%.1f/s", perSecond)
    perSecond > 0 -> String.format(Locale.ROOT, "%.2f/s", perSecond)
    else -> "0/s"
}

/** A duration in milliseconds as "412 ms", "6.8 ms" or "0.6 ms". */
fun formatMs(ms: Double): String =
    if (ms >= 10) String.format(Locale.ROOT, "%.0f ms", ms) else String.format(Locale.ROOT, "%.1f ms", ms)
