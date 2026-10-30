package name.levis.ichor.wear

import android.content.ComponentName
import android.content.Context
import android.net.Uri
import androidx.wear.tiles.TileService
import androidx.wear.watchface.complications.datasource.ComplicationDataSourceUpdateRequester
import com.google.android.gms.wearable.DataItem
import com.google.android.gms.wearable.DataMapItem
import com.google.android.gms.wearable.PutDataRequest
import com.google.android.gms.wearable.Wearable
import name.levis.ichor.widget.FleetTally
import name.levis.ichor.widget.WEAR_PAYLOAD_KEY
import name.levis.ichor.widget.WEAR_SNAPSHOT_PATH
import name.levis.ichor.widget.WearCluster
import name.levis.ichor.widget.WearPayload
import name.levis.ichor.widget.decodeWearPayload

/** The phone's last payload on the watch (counts and labels only), for the tile and the complication. */
object WatchSnapshotStore {
    private const val FILE = "ichor-wear-snapshot"
    private const val KEY = "payload"

    fun load(context: Context): WearPayload? =
        prefs(context).getString(KEY, null)?.let { decodeWearPayload(it.encodeToByteArray()) }

    /** The cluster on screen on the phone, or null before the first payload. */
    fun active(context: Context): WearCluster? = load(context)?.clusters?.firstOrNull()

    /** The whole fleet's tally, or null before a phone app that sends one. */
    fun fleet(context: Context): FleetTally? = load(context)?.fleet

    fun save(context: Context, bytes: ByteArray?) {
        val edit = prefs(context).edit()
        if (bytes == null) edit.remove(KEY) else edit.putString(KEY, bytes.decodeToString())
        edit.apply()
    }

    fun has(context: Context): Boolean = prefs(context).contains(KEY)

    /** The payload bytes of the phone's `/snapshot` data item. */
    fun bytesOf(item: DataItem): ByteArray? =
        runCatching { DataMapItem.fromDataItem(item).dataMap.getByteArray(WEAR_PAYLOAD_KEY) }.getOrNull()

    /**
     * Reads the data item the phone wrote before this app was installed (no change event comes for
     * it), then calls [done]; at once when one is stored already or the Data Layer fails.
     */
    fun fetchIfMissing(context: Context, done: () -> Unit) {
        if (has(context)) return done()
        val uri = Uri.Builder().scheme(PutDataRequest.WEAR_URI_SCHEME).path(WEAR_SNAPSHOT_PATH).build()
        runCatching {
            Wearable.getDataClient(context).getDataItems(uri)
                .addOnSuccessListener { buffer ->
                    val bytes = buffer.firstOrNull()?.let(::bytesOf)
                    buffer.release()
                    if (bytes != null) save(context, bytes)
                    done()
                }
                .addOnFailureListener { done() }
        }.onFailure { done() }
    }

    /** Redraws the tiles and the complications from the stored payload. */
    fun requestRedraw(context: Context) {
        listOf(ClusterTileService::class.java, FleetTileService::class.java).forEach { tile ->
            runCatching { TileService.getUpdater(context).requestUpdate(tile) }
        }
        listOf(ClusterComplicationService::class.java, FleetComplicationService::class.java).forEach { source ->
            runCatching { ComplicationDataSourceUpdateRequester.create(context, ComponentName(context, source)).requestUpdateAll() }
        }
    }

    private fun prefs(context: Context) = context.getSharedPreferences(FILE, Context.MODE_PRIVATE)
}
