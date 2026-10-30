package name.levis.ichor.wear

import androidx.wear.watchface.complications.data.ComplicationData
import androidx.wear.watchface.complications.data.ComplicationType
import androidx.wear.watchface.complications.data.PlainComplicationText
import androidx.wear.watchface.complications.data.RangedValueComplicationData
import androidx.wear.watchface.complications.data.ShortTextComplicationData
import androidx.wear.watchface.complications.datasource.ComplicationRequest
import androidx.wear.watchface.complications.datasource.SuspendingComplicationDataSourceService
import name.levis.ichor.widget.FleetTally
import name.levis.ichor.widget.isStaleAt
import name.levis.ichor.widget.toneOf

/**
 * How many clusters need attention, from the phone's last check: "2/6" as a short text under
 * the tone in a word, or as a ranged value (needing attention out of all clusters); the newest
 * check's age once stale. Counts only, no cluster name.
 */
class FleetComplicationService : SuspendingComplicationDataSourceService() {

    override suspend fun onComplicationRequest(request: ComplicationRequest): ComplicationData? =
        data(request.complicationType, WatchSnapshotStore.fleet(this), System.currentTimeMillis())

    override fun getPreviewData(type: ComplicationType): ComplicationData? =
        data(type, FleetTally(bad = 1, warn = 1, ok = 4, total = 6, takenAt = 1), 1)

    private fun data(type: ComplicationType, t: FleetTally?, now: Long): ComplicationData? = when (type) {
        ComplicationType.SHORT_TEXT -> shortText(t, now)
        ComplicationType.RANGED_VALUE -> ranged(t, now)
        else -> null
    }

    private fun shortText(t: FleetTally?, now: Long): ComplicationData {
        if (t == null || t.total == 0) return noData()
        return ShortTextComplicationData.Builder(plain(countText(t, now)), plain(fleetDescription(resources, t, now)))
            .setTitle(plain(toneText(resources, toneOf(t, now))))
            .build()
    }

    private fun ranged(t: FleetTally?, now: Long): ComplicationData {
        if (t == null || t.total == 0) {
            return RangedValueComplicationData.Builder(0f, 0f, 1f, plain(getString(R.string.wear_no_data))).setText(plain("–")).build()
        }
        return RangedValueComplicationData.Builder(t.needAttention.toFloat(), 0f, t.total.toFloat(), plain(fleetDescription(resources, t, now)))
            .setText(plain(countText(t, now)))
            .setTitle(plain(toneText(resources, toneOf(t, now))))
            .build()
    }

    /** "2/6", or the newest check's age ("3h") once stale. */
    private fun countText(t: FleetTally, now: Long): String =
        if (t.takenAt > 0 && isStaleAt(t.takenAt, now)) shortAge(now - t.takenAt) else getString(R.string.wear_fleet_short, t.needAttention, t.total)

    private fun noData(): ComplicationData =
        ShortTextComplicationData.Builder(plain("–"), plain(getString(R.string.wear_no_data)))
            .setTitle(plain(getString(R.string.wear_app_name)))
            .build()

    private fun plain(text: String) = PlainComplicationText.Builder(text).build()
}
