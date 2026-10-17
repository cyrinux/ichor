package name.levis.ichor.wear

import android.content.Context
import name.levis.ichor.widget.WearPayload

/** The open-source builds carry no Google library, so no Data Layer: nothing reaches a watch. */
@Suppress("UNUSED_PARAMETER")
fun createWearSnapshotSync(context: Context): WearSnapshotSync = NoWearSnapshotSync

private object NoWearSnapshotSync : WearSnapshotSync {
    override fun send(payload: WearPayload) = Unit
}
