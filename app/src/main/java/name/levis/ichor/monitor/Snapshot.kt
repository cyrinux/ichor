package name.levis.ichor.monitor

import name.levis.ichor.model.ClusterOverview
import name.levis.ichor.model.DataServices
import name.levis.ichor.model.EtcdOverview
import name.levis.ichor.model.NodeHealth
import name.levis.ichor.model.health
import kotlinx.serialization.Serializable

/** What background monitoring last saw; drives both alerts (by diffing) and the widget. */
@Serializable
data class ClusterSnapshot(
    val context: String,
    val takenAt: Long,
    val nodes: Map<String, NodeState>,
    val etcdAlarms: List<String> = emptyList(),
    val etcdChecked: Boolean = false,
    val certNotAfter: Long = 0,
    /** Epoch day of the last certificate-expiry warning, so it fires at most once a day. */
    val lastCertWarnDay: Long = -1,
    /** The cluster's context fingerprint: the widget shows the name the user gave it. */
    val fingerprint: String = "",
    /** Watching Longhorn, Garage and CloudNativePG was on for this check (opt-in). */
    val dataWatched: Boolean = false,
    /** Their health could be read this time. */
    val dataChecked: Boolean = false,
    /**
     * Data-service issues ("system|label" → severity, see [dataIssuesOf]): what was observed in a
     * fresh snapshot; after [evaluate], the ones already notified (or present at the baseline).
     */
    val dataIssues: Map<String, String> = emptyMap(),
    /** Warnings seen once and not notified yet: a short rebuild after a reboot should not alert. */
    val dataPending: List<String> = emptyList(),
) {
    val readyCount: Int get() = nodes.values.count { it.health == NodeHealth.READY }
    val notReadyCount: Int get() = nodes.values.count { it.health == NodeHealth.NOT_READY }
    val unreachableCount: Int get() = nodes.values.count { it.health == NodeHealth.UNREACHABLE }

    /** No node answered: most likely the phone is off the cluster's network (VPN, home LAN). */
    val unreachableAsAWhole: Boolean get() = nodes.isNotEmpty() && unreachableCount == nodes.size
}

@Serializable
data class NodeState(
    val hostname: String,
    val health: NodeHealth,
    val reason: String = "",
)

fun snapshotOf(
    overview: ClusterOverview,
    etcd: EtcdOverview?,
    certNotAfter: Long,
    takenAt: Long,
    fingerprint: String = "",
    /** Data services watched ([dataServices] null when watched but unreadable). */
    dataWatched: Boolean = false,
    dataServices: DataServices? = null,
): ClusterSnapshot = ClusterSnapshot(
    dataWatched = dataWatched,
    dataChecked = dataWatched && dataServices != null,
    dataIssues = dataServices?.takeIf { dataWatched }?.let(::dataIssuesOf).orEmpty(),
    context = overview.context,
    fingerprint = fingerprint,
    takenAt = takenAt,
    nodes = overview.nodes.associate { n ->
        val reason = n.error ?: n.unmetConditions.joinToString("; ") { "${it.name}: ${it.reason}" }
        n.node to NodeState(n.hostname, n.health, reason)
    },
    etcdAlarms = etcd?.alarms?.map { "${it.memberId}:${it.alarm}" }.orEmpty().sorted(),
    etcdChecked = etcd != null && etcd.error == null,
    certNotAfter = certNotAfter,
)
