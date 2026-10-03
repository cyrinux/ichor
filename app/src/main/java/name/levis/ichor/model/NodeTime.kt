package name.levis.ichor.model

import kotlinx.serialization.Serializable
import kotlin.math.abs

// Mirrors go/ichorgo/nodetime.go.

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

/** How far the node's clock is off; null when the node could not be asked (unreachable). */
val NodeTime.drift: DriftLevel?
    get() = when {
        error != null -> null
        abs(offsetMs) >= DRIFT_BAD_MS -> DriftLevel.BAD
        abs(offsetMs) >= DRIFT_WARN_MS -> DriftLevel.WARN
        else -> DriftLevel.OK
    }

/**
 * The cluster-wide verdict of the clock drift card. [level] covers reachable nodes only
 * (null when none answered), so an unreachable node never reads as "Out of sync".
 */
data class DriftSummary(
    val level: DriftLevel?,
    val reachable: Int,
    val unreachable: Int,
    /** Reachable nodes at or past [DRIFT_WARN_MS]. */
    val drifting: Int,
    /** Largest absolute offset over reachable nodes, null when none answered. */
    val maxOffsetMs: Long?,
) {
    /** The per-node list starts expanded only when a reachable clock is off. */
    val expandedByDefault: Boolean get() = level != null && level != DriftLevel.OK
}

fun driftSummary(nodes: List<NodeTime>): DriftSummary {
    val (reachable, unreachable) = nodes.partition { it.error == null }
    return DriftSummary(
        level = reachable.mapNotNull { it.drift }.maxOrNull(),
        reachable = reachable.size,
        unreachable = unreachable.size,
        drifting = reachable.count { it.drift != DriftLevel.OK },
        maxOffsetMs = reachable.maxOfOrNull { abs(it.offsetMs) },
    )
}

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

/** "±9 ms": the size of the largest offset, sign-free (used for the cluster summary). */
fun formatMaxOffset(offsetMs: Long): String = "±" + formatOffset(abs(offsetMs)).drop(1)
