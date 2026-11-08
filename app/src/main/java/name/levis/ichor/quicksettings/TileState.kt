package name.levis.ichor.quicksettings

import name.levis.ichor.model.ConfigSummary
import name.levis.ichor.monitor.MonitorState
import name.levis.ichor.widget.nodeCounts
import name.levis.ichor.widget.widgetKeys
import name.levis.ichor.widget.widgetLabel
import name.levis.ichor.widget.widgetSnapshot

/** What the Quick Settings tile says of the cluster on screen. */
enum class TileHealth { READY, DEGRADED, NO_CHECK, NO_CLUSTER }

/** The tile's content: [label] is the cluster's name, null until the monitor checked it. */
data class TileView(val health: TileHealth, val label: String?, val ready: Int, val total: Int)

/**
 * The tile of the cluster on screen, from the snapshot the home widget shows ([state], the last
 * background check): no network call, nothing unsealed. [summary] and [activeContext] are null
 * while the config is locked; the monitor's last active cluster stands in for them then.
 */
fun tileView(state: MonitorState, summary: ConfigSummary?, activeContext: String?, names: Map<String, String>, masked: Boolean): TileView {
    val snapshot = widgetSnapshot(state, widgetKeys(null, summary, activeContext, state.active))
    if (snapshot == null) {
        val noCluster = summary?.contexts.isNullOrEmpty() && state.active.isBlank() && state.clusters.isEmpty()
        return TileView(if (noCluster) TileHealth.NO_CLUSTER else TileHealth.NO_CHECK, null, 0, 0)
    }
    val counts = nodeCounts(snapshot)
    val health = if (counts.total > 0 && counts.ready == counts.total) TileHealth.READY else TileHealth.DEGRADED
    return TileView(health, widgetLabel(snapshot, names, masked), counts.ready, counts.total)
}
