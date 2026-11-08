package name.levis.ichor.wear

import com.google.android.gms.wearable.DataEvent
import com.google.android.gms.wearable.DataEventBuffer
import com.google.android.gms.wearable.WearableListenerService
import name.levis.ichor.widget.WEAR_SNAPSHOT_PATH

/** Keeps the phone's latest `/snapshot` and redraws the tile and the complication. */
class WearDataListenerService : WearableListenerService() {
    override fun onDataChanged(dataEvents: DataEventBuffer) {
        var changed = false
        dataEvents.forEach { event ->
            val item = event.dataItem
            if (item.uri.path != WEAR_SNAPSHOT_PATH) return@forEach
            when (event.type) {
                DataEvent.TYPE_CHANGED -> WatchSnapshotStore.save(this, WatchSnapshotStore.bytesOf(item))
                DataEvent.TYPE_DELETED -> WatchSnapshotStore.save(this, null)
                else -> return@forEach
            }
            changed = true
        }
        if (changed) WatchSnapshotStore.requestRedraw(this)
    }
}
