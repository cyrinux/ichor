package name.levis.ichor.wear

import name.levis.ichor.model.ConfigSummary
import name.levis.ichor.monitor.MonitorState
import name.levis.ichor.widget.WearCluster
import name.levis.ichor.widget.WearPayload
import name.levis.ichor.widget.nodeCounts
import name.levis.ichor.widget.widgetKeys
import name.levis.ichor.widget.widgetLabel
import name.levis.ichor.widget.widgetSnapshot

/**
 * Hands the Wear OS tile and complication what the widget shows. The Play build writes it to
 * the Data Layer (src/play, when Google Play services are there); the open-source builds have no
 * Google library and do nothing (src/foss). Created by `createWearSnapshotSync`.
 */
interface WearSnapshotSync {
    fun send(payload: WearPayload)
}

/**
 * What the watch gets of the monitor's [state]: the cluster on screen first (found like the
 * widget's, see [widgetKeys]), then the others by label. Labels follow the widget's rules
 * ([widgetLabel]: no given name in screenshot mode, [masked]); only counts and the check's time
 * go along, never a hostname, endpoint or credential.
 */
fun wearPayload(
    state: MonitorState,
    summary: ConfigSummary?,
    activeContext: String?,
    names: Map<String, String>,
    masked: Boolean,
): WearPayload {
    val active = widgetSnapshot(state, widgetKeys(null, summary, activeContext, state.active))
    val others = state.clusters.values.filter { it !== active }
        .map { WearCluster(widgetLabel(it, names, masked), it.takenAt, nodeCounts(it)) }
        .sortedBy { it.label }
    val first = active?.let { listOf(WearCluster(widgetLabel(it, names, masked), it.takenAt, nodeCounts(it))) }.orEmpty()
    return WearPayload(first + others)
}
