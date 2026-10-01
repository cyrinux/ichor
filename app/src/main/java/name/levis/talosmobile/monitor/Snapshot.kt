package name.levis.talosmobile.monitor

import name.levis.talosmobile.model.ClusterOverview
import name.levis.talosmobile.model.EtcdOverview
import name.levis.talosmobile.model.NodeHealth
import name.levis.talosmobile.model.health
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
) {
    val readyCount: Int get() = nodes.values.count { it.health == NodeHealth.READY }
    val notReadyCount: Int get() = nodes.values.count { it.health == NodeHealth.NOT_READY }
    val unreachableCount: Int get() = nodes.values.count { it.health == NodeHealth.UNREACHABLE }
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
): ClusterSnapshot = ClusterSnapshot(
    context = overview.context,
    takenAt = takenAt,
    nodes = overview.nodes.associate { n ->
        val reason = n.error ?: n.unmetConditions.joinToString("; ") { "${it.name}: ${it.reason}" }
        n.node to NodeState(n.hostname, n.health, reason)
    },
    etcdAlarms = etcd?.alarms?.map { "${it.memberId}:${it.alarm}" }.orEmpty().sorted(),
    etcdChecked = etcd != null && etcd.error == null,
    certNotAfter = certNotAfter,
)
