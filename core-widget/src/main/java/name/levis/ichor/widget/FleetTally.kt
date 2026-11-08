package name.levis.ichor.widget

import kotlinx.serialization.Serializable

/**
 * How many clusters of the fleet are in each tone after the phone's last check, the count the
 * fleet widget, the Wear OS fleet tile and its complication show. [takenAt] is the newest
 * check's time (unix ms, 0: none yet): once it is stale, the whole tally is. Counts only, no
 * cluster name, so a watch face can show it as is.
 */
@Serializable
data class FleetTally(
    val bad: Int = 0,
    val warn: Int = 0,
    val unknown: Int = 0,
    val ok: Int = 0,
    val total: Int = 0,
    val takenAt: Long = 0,
) {
    /** Down, critical or degraded clusters. */
    val needAttention: Int get() = bad + warn
}

/**
 * The colour of [t] at [now], as the Go core ranks a fleet (bad, warn, unknown, ok, worst
 * first); [Tone.UNKNOWN] for no fleet or a stale newest check.
 */
fun toneOf(t: FleetTally?, now: Long): Tone = when {
    t == null || t.total == 0 || isStaleAt(t.takenAt.takeIf { it > 0 }, now) -> Tone.UNKNOWN
    t.bad > 0 -> Tone.BAD
    t.warn > 0 -> Tone.WARN
    t.unknown > 0 -> Tone.UNKNOWN
    t.ok > 0 -> Tone.OK
    else -> Tone.UNKNOWN
}
