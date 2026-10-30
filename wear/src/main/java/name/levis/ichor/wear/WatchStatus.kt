package name.levis.ichor.wear

import android.content.res.Resources
import android.text.format.DateUtils
import name.levis.ichor.widget.FleetTally
import name.levis.ichor.widget.StatusItem
import name.levis.ichor.widget.Tone
import name.levis.ichor.widget.isStaleAt
import name.levis.ichor.widget.toneOf

private const val MINUTE_MS = 60_000L
private const val HOUR_MS = 60 * MINUTE_MS
private const val DAY_MS = 24 * HOUR_MS

/** "45m", "3h", "2d": the age of a check in the few characters of a short-text complication. */
fun shortAge(ageMs: Long): String = when {
    ageMs < HOUR_MS -> "${maxOf(1, ageMs / MINUTE_MS)}m"
    ageMs < DAY_MS -> "${ageMs / HOUR_MS}h"
    else -> "${ageMs / DAY_MS}d"
}

/** The widget's colours (ClusterWidget.kt), as ARGB. */
object WatchColors {
    const val PRIMARY = 0xFFE8EEF5.toInt()
    const val SECONDARY = 0xFF93A3B8.toInt()
    const val DIMMED = 0x80E8EEF5.toInt()
    const val OK = 0xFF5BD18B.toInt()
    const val WARN = 0xFFF2C14E.toInt()
    const val BAD = 0xFFF0716B.toInt()

    fun of(tone: Tone): Int = when (tone) {
        Tone.OK -> OK
        Tone.WARN -> WARN
        Tone.BAD -> BAD
        Tone.UNKNOWN -> SECONDARY
    }
}

/** The label of one status item, as the phone widget words it. */
fun statusText(res: Resources, item: StatusItem): String = when (item) {
    is StatusItem.NotReady -> res.getQuantityString(R.plurals.wear_not_ready, item.count, item.count)
    is StatusItem.Unreachable -> res.getQuantityString(R.plurals.wear_unreachable, item.count, item.count)
    StatusItem.AllReady -> res.getString(R.string.wear_all_ready)
    is StatusItem.Etcd -> if (item.alarms == 0) {
        res.getString(R.string.wear_etcd_no_alarms)
    } else {
        res.getQuantityString(R.plurals.wear_etcd_alarms, item.alarms, item.alarms)
    }
}

/** The tone in a word, for the complication's title and screen readers. */
fun toneText(res: Resources, tone: Tone): String = res.getString(
    when (tone) {
        Tone.OK -> R.string.wear_tone_ok
        Tone.WARN -> R.string.wear_tone_warn
        Tone.BAD -> R.string.wear_tone_bad
        Tone.UNKNOWN -> R.string.wear_tone_stale
    },
)

/** "Updated 2 hours ago" under a stale count. */
fun staleText(res: Resources, takenAt: Long, now: Long): String =
    res.getString(R.string.wear_updated, DateUtils.getRelativeTimeSpanString(takenAt, now, DateUtils.MINUTE_IN_MILLIS))

/** "need attention" under a fleet count, "All 6 fine", or how many clusters were not checked lately. */
fun fleetDetail(res: Resources, t: FleetTally): String = when {
    t.needAttention > 0 -> res.getString(R.string.wear_fleet_need_attention)
    t.unknown > 0 -> res.getString(R.string.wear_fleet_unchecked, t.unknown)
    else -> res.getString(R.string.wear_fleet_all_fine, t.total)
}

/** What a screen reader says of the fleet tally: "2 of 6 need attention, Alert" (and the check's age once stale). */
fun fleetDescription(res: Resources, t: FleetTally, now: Long): String = buildList {
    add(res.getString(R.string.wear_fleet_attention, t.needAttention, t.total))
    add(toneText(res, toneOf(t, now)))
    if (t.takenAt > 0 && isStaleAt(t.takenAt, now)) add(staleText(res, t.takenAt, now))
}.joinToString(", ")

/** Under a stale fleet count: the newest check's age, or how many were never checked. */
fun fleetStaleText(res: Resources, t: FleetTally, now: Long): String =
    if (t.takenAt > 0) staleText(res, t.takenAt, now) else res.getString(R.string.wear_fleet_unchecked, t.unknown)
