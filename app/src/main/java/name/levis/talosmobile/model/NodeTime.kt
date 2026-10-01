package name.levis.talosmobile.model

import kotlinx.serialization.Serializable
import kotlin.math.abs

// Mirrors go/talosmobile/nodetime.go.

@Serializable
data class NodeTime(
    val node: String,
    /** NTP server the node compared its clock against. */
    val server: String = "",
    /** Unix millis, node clock. */
    val localTime: Long = 0,
    /** Unix millis, NTP server clock. */
    val remoteTime: Long = 0,
    /** remoteTime - localTime. */
    val offsetMs: Long = 0,
    val error: String? = null,
)

@Serializable
data class ClusterTime(val context: String = "", val nodes: List<NodeTime> = emptyList())

/** Offset from which a clock is flagged, then considered bad. */
const val DRIFT_WARN_MS = 500L
const val DRIFT_BAD_MS = 5_000L

enum class DriftLevel { OK, WARN, BAD }

val NodeTime.drift: DriftLevel
    get() = when {
        error != null -> DriftLevel.BAD
        abs(offsetMs) >= DRIFT_BAD_MS -> DriftLevel.BAD
        abs(offsetMs) >= DRIFT_WARN_MS -> DriftLevel.WARN
        else -> DriftLevel.OK
    }

/** The worst level over [nodes]; OK when empty. */
fun worstDrift(nodes: List<NodeTime>): DriftLevel = nodes.maxOfOrNull { it.drift } ?: DriftLevel.OK

/** "+12 ms", "−1.25 s", "+3 min 4 s": a signed, readable clock offset. */
fun formatOffset(offsetMs: Long): String {
    val sign = if (offsetMs < 0) "−" else "+"
    val ms = abs(offsetMs)
    return sign + when {
        ms < 1_000 -> "$ms ms"
        ms < 60_000 -> String.format(java.util.Locale.ROOT, "%.2f s", ms / 1000.0)
        else -> "${ms / 60_000} min ${(ms % 60_000) / 1000} s"
    }
}
