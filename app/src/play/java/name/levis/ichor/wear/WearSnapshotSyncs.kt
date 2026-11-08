package name.levis.ichor.wear

import android.content.Context
import com.google.android.gms.common.ConnectionResult
import com.google.android.gms.common.GoogleApiAvailability
import com.google.android.gms.wearable.PutDataMapRequest
import com.google.android.gms.wearable.Wearable
import name.levis.ichor.widget.WEAR_PAYLOAD_KEY
import name.levis.ichor.widget.WEAR_SNAPSHOT_PATH
import name.levis.ichor.widget.WearPayload
import name.levis.ichor.widget.encodeWearPayload

/** The Wear OS Data Layer: the watch app (`:wear`) reads the `/snapshot` data item. */
fun createWearSnapshotSync(context: Context): WearSnapshotSync = PlayWearSnapshotSync(context.applicationContext)

private class PlayWearSnapshotSync(private val context: Context) : WearSnapshotSync {
    /** Without Google Play services (a de-Googled phone) there is no Data Layer: never call it. */
    private val available: Boolean by lazy {
        runCatching {
            GoogleApiAvailability.getInstance().isGooglePlayServicesAvailable(context) == ConnectionResult.SUCCESS
        }.getOrDefault(false)
    }

    override fun send(payload: WearPayload) {
        if (!available) return
        // An identical payload changes nothing on the watch (the Data Layer drops it), so no
        // time of sending goes in. No watch, or no Wearable API: the failed task is ignored.
        runCatching {
            val request = PutDataMapRequest.create(WEAR_SNAPSHOT_PATH)
                .apply { dataMap.putByteArray(WEAR_PAYLOAD_KEY, encodeWearPayload(payload)) }
                .asPutDataRequest()
                .setUrgent()
            Wearable.getDataClient(context).putDataItem(request).addOnFailureListener { }
        }
    }
}
