package name.levis.ichor.wear

import androidx.wear.watchface.complications.data.ComplicationData
import androidx.wear.watchface.complications.data.ComplicationType
import androidx.wear.watchface.complications.data.PlainComplicationText
import androidx.wear.watchface.complications.data.ShortTextComplicationData
import androidx.wear.watchface.complications.datasource.ComplicationRequest
import androidx.wear.watchface.complications.datasource.SuspendingComplicationDataSourceService
import name.levis.ichor.widget.NodeCounts
import name.levis.ichor.widget.WearCluster
import name.levis.ichor.widget.isStaleAt
import name.levis.ichor.widget.toneOf

/**
 * The cluster on screen on the phone as a short text: the ready count under the tone in a word
 * (a watch face draws a complication in its own colours); the check's age once stale.
 */
class ClusterComplicationService : SuspendingComplicationDataSourceService() {

    override suspend fun onComplicationRequest(request: ComplicationRequest): ComplicationData? {
        if (request.complicationType != ComplicationType.SHORT_TEXT) return null
        return shortText(WatchSnapshotStore.active(this), System.currentTimeMillis())
    }

    override fun getPreviewData(type: ComplicationType): ComplicationData? {
        if (type != ComplicationType.SHORT_TEXT) return null
        val sample = WearCluster("lab", 0, NodeCounts(total = 3, ready = 3, notReady = 0, unreachable = 0, etcdChecked = true, etcdAlarms = 0))
        return shortText(sample, 0)
    }

    private fun shortText(c: WearCluster?, now: Long): ComplicationData {
        if (c == null) {
            val none = getString(R.string.wear_no_data)
            return ShortTextComplicationData.Builder(plain("–"), plain(none)).setTitle(plain(getString(R.string.wear_app_name))).build()
        }
        val tone = toneOf(c.counts, c.takenAt, now)
        val count = getString(R.string.wear_ready_short, c.counts.ready, c.counts.total)
        val stale = isStaleAt(c.takenAt, now)
        val text = if (stale) shortAge(now - c.takenAt) else count
        val description = buildList {
            add(c.label)
            add(getString(R.string.wear_ready_count, c.counts.ready, c.counts.total))
            add(toneText(resources, tone))
            if (stale) add(staleText(resources, c.takenAt, now))
        }.joinToString(", ")
        return ShortTextComplicationData.Builder(plain(text), plain(description))
            .setTitle(plain(toneText(resources, tone)))
            .build()
    }

    private fun plain(text: String) = PlainComplicationText.Builder(text).build()
}
