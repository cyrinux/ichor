package name.levis.ichor.wear

import name.levis.ichor.model.ConfigSummary
import name.levis.ichor.model.NodeHealth
import name.levis.ichor.monitor.ClusterSnapshot
import name.levis.ichor.monitor.MonitorState
import name.levis.ichor.monitor.tallyAt
import name.levis.ichor.widget.WearCluster
import name.levis.ichor.widget.WearNode
import name.levis.ichor.widget.WearNodeHealth
import name.levis.ichor.widget.WearPayload
import name.levis.ichor.widget.nodeCounts
import name.levis.ichor.widget.widgetKeys
import name.levis.ichor.widget.widgetLabel
import name.levis.ichor.widget.trimmedWearPayload
import name.levis.ichor.widget.wearNodeName
import name.levis.ichor.widget.wearNodeProblem
import name.levis.ichor.widget.wearNodes
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
 * ([widgetLabel]: no given name in screenshot mode, [masked]); counts, the check's time and the
 * nodes go along ([wearNodesOf]: short host names, none when masked; an error's category, never
 * its text), never an address, endpoint or credential. The fleet's tally at [now] goes too, once
 * a check ranked it (counts only: the fleet tile and complication name no cluster).
 */
fun wearPayload(
    state: MonitorState,
    summary: ConfigSummary?,
    activeContext: String?,
    names: Map<String, String>,
    masked: Boolean,
    now: Long = System.currentTimeMillis(),
): WearPayload {
    val active = widgetSnapshot(state, widgetKeys(null, summary, activeContext, state.active))
    val cluster = { s: ClusterSnapshot -> WearCluster(widgetLabel(s, names, masked), s.takenAt, nodeCounts(s), wearNodesOf(s, masked)) }
    val others = state.clusters.values.filter { it !== active }.map(cluster).sortedBy { it.label }
    val first = active?.let { listOf(cluster(it)) }.orEmpty()
    val fleet = state.fleet.takeIf { it.rows.isNotEmpty() }?.tallyAt(now)
    return trimmedWearPayload(WearPayload(first + others, fleet))
}

/** [s]'s nodes for the watch, the ones down first: no address, no reason, an error's category. */
fun wearNodesOf(s: ClusterSnapshot, masked: Boolean): List<WearNode> = wearNodes(
    s.nodes.values.map { n ->
        val health = when (n.health) {
            NodeHealth.READY -> WearNodeHealth.READY
            NodeHealth.NOT_READY -> WearNodeHealth.NOT_READY
            NodeHealth.UNREACHABLE -> WearNodeHealth.UNREACHABLE
        }
        val problem = if (health == WearNodeHealth.UNREACHABLE) wearNodeProblem(n.reason) else null
        WearNode(wearNodeName(n.hostname, masked), health, problem)
    },
)
