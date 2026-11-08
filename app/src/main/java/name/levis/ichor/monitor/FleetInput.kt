package name.levis.ichor.monitor

import kotlinx.serialization.Serializable
import name.levis.ichor.model.ClusterLabels
import name.levis.ichor.model.ConfigSummary
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.NodeHealth
import name.levis.ichor.model.clusterTagsOf
import name.levis.ichor.model.isDemo
import name.levis.ichor.model.isKube
import name.levis.ichor.widget.widgetLabel

/** A node of a [FleetEntry]: its hostname (blank in screenshot mode) and health. */
@Serializable
data class FleetNode(val hostname: String, val health: NodeHealth)

/**
 * One cluster's last background check as the Go core's FleetSummary reads it: [key] the
 * cluster, [name] its label as shown (masked in screenshot mode), [context] the context to
 * switch to, [takenAt] unix ms (0: never checked), [failures] the checks missed in a row.
 */
@Serializable
data class FleetEntry(
    val key: String,
    val name: String,
    val context: String,
    val kube: Boolean = false,
    val tags: List<String> = emptyList(),
    val takenAt: Long = 0,
    val failures: Int = 0,
    val nodes: Map<String, FleetNode> = emptyMap(),
    val etcdChecked: Boolean = false,
    val etcdAlarms: List<String> = emptyList(),
    val amChecked: Boolean = false,
    val amIssues: Map<String, String> = emptyMap(),
    val checkupChecked: Boolean = false,
    val checkupIssues: Map<String, String> = emptyMap(),
    val dataChecked: Boolean = false,
    val dataIssues: Map<String, String> = emptyMap(),
    val gitopsChecked: Boolean = false,
    val gitopsIssues: Map<String, String> = emptyMap(),
    val storageChecked: Boolean = false,
    val storageIssues: Map<String, String> = emptyMap(),
    val sensorChecked: Boolean = false,
    val sensorIssues: Map<String, String> = emptyMap(),
    val kubespanChecked: Boolean = false,
    val kubespanIssues: Map<String, String> = emptyMap(),
    val pluginChecked: Boolean = false,
    val pluginIssues: Map<String, String> = emptyMap(),
)

@Serializable
data class FleetEntries(val clusters: List<FleetEntry>)

/** Whether the only clusters on the phone are the demo: the fleet screen shows the demo's fleet then. */
fun demoFleetOnly(summary: ConfigSummary?): Boolean =
    summary != null && summary.contexts.isNotEmpty() && summary.contexts.all { it.isDemo }

/**
 * The fleet screen's input: one entry per cluster of [summary] (the demo left out: its own
 * snapshot exists only while it is on screen), from the snapshot the monitor kept of any of its
 * contexts; a cluster with none yet has takenAt 0 (never checked). Without a [summary] (the
 * config not loaded yet), one per stored snapshot. Names as the app shows them ([names] given
 * by the user, unless [masked]); in screenshot mode the hostnames are left out too, since the
 * snapshot keeps them as read. [tags] by fingerprint, the user's words: shown even when [masked].
 */
fun fleetEntries(
    state: MonitorState,
    summary: ConfigSummary?,
    names: Map<String, String>,
    masked: Boolean,
    tags: Map<String, List<String>> = emptyMap(),
): List<FleetEntry> {
    if (summary == null) {
        return state.clusters.toSortedMap().map { (key, snapshot) ->
            entryOf(key, widgetLabel(snapshot, names, masked), snapshot.context, snapshot.kube, snapshot, state.reach[key], masked)
                .copy(tags = tags[snapshot.fingerprint].orEmpty())
        }
    }
    val labels = ClusterLabels(names, masked)
    return summary.contexts.filterNot { it.isDemo }.groupBy(::clusterKeyOf).map { (key, group) ->
        val checked = group.firstOrNull { monitorKeyOf(it) in state.clusters }
        val context: ContextSummary = checked ?: group.first()
        val monitorKey = monitorKeyOf(context)
        val snapshot = state.clusters[monitorKey]
        entryOf(key, labels.of(context), context.name, context.isKube, snapshot, state.reach[monitorKey], masked)
            .copy(tags = clusterTagsOf(tags, group.map { it.fingerprint }))
    }
}

private fun entryOf(
    key: String,
    name: String,
    context: String,
    kube: Boolean,
    s: ClusterSnapshot?,
    reach: Reach?,
    masked: Boolean,
): FleetEntry {
    if (s == null) return FleetEntry(key = key, name = name, context = context, kube = kube, failures = reach?.failures ?: 0)
    return FleetEntry(
        key = key,
        name = name,
        context = context,
        kube = kube || s.kube,
        takenAt = s.takenAt,
        failures = reach?.failures ?: 0,
        nodes = s.nodes.mapValues { (_, node) -> FleetNode(if (masked) "" else node.hostname, node.health) },
        etcdChecked = s.etcdChecked,
        etcdAlarms = s.etcdAlarms,
        amChecked = s.amChecked,
        amIssues = s.amIssues,
        checkupChecked = s.checkupChecked,
        checkupIssues = s.checkupIssues,
        dataChecked = s.dataChecked,
        dataIssues = s.dataIssues,
        gitopsChecked = s.gitopsChecked,
        gitopsIssues = s.gitopsIssues,
        storageChecked = s.storageChecked,
        storageIssues = s.storageIssues,
        sensorChecked = s.sensorChecked,
        sensorIssues = s.sensorIssues,
        kubespanChecked = s.kubespanChecked,
        kubespanIssues = s.kubespanIssues,
        pluginChecked = s.pluginChecked,
        pluginIssues = s.pluginIssues,
    )
}
