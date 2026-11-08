package name.levis.ichor.monitor

import name.levis.ichor.data.OfflineCache
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.model.ClusterOverview
import name.levis.ichor.model.KubeNodeInfo
import name.levis.ichor.model.KubeNodesOverview
import name.levis.ichor.model.NodeHealth
import name.levis.ichor.model.NodeOverview

// What the home shows from the last background check before the cluster answers (a seed):
// the nodes and their health, nothing else. Never kept as a result.

/** The overview the last background check saw: each node, reachable and ready or not, with why not. */
fun ClusterSnapshot.toOverviewSeed(): ClusterOverview = ClusterOverview(
    context = context,
    nodes = nodes.map { (node, state) ->
        val reachable = state.health != NodeHealth.UNREACHABLE
        NodeOverview(
            node = node,
            hostname = state.hostname,
            reachable = reachable,
            ready = state.health == NodeHealth.READY,
            error = state.reason.takeIf { !reachable && it.isNotBlank() },
        )
    }.sortedBy { it.hostname.ifBlank { it.node } },
)

/** The Kubernetes nodes the last background check of a kubeconfig cluster saw. */
fun ClusterSnapshot.toKubeNodesSeed(): KubeNodesOverview = KubeNodesOverview(
    nodes = nodes.values.map { state ->
        KubeNodeInfo(
            name = state.hostname,
            ready = state.health == NodeHealth.READY,
            pressure = state.reason.split("; ").filter { it.isNotBlank() },
        )
    }.sortedBy { it.name },
)

/**
 * The last background check of the cluster [fingerprint] (null: the demo, never watched), mapped
 * by [seed], when it is recent, of the right kind ([kube]) and saw nodes. None in screenshot mode
 * ([masked]): the check keeps the real names.
 */
fun <T> MonitorStore.seed(
    fingerprint: String?,
    kube: Boolean,
    masked: Boolean,
    now: Long = System.currentTimeMillis(),
    seed: (ClusterSnapshot) -> T,
): TalosRepository.Timed<T>? {
    if (fingerprint == null || masked) return null
    val snapshot = snapshot(fingerprint) ?: return null
    if (snapshot.kube != kube || snapshot.nodes.isEmpty() || now - snapshot.takenAt > OfflineCache.MAX_AGE_MS) return null
    return TalosRepository.Timed(seed(snapshot), snapshot.takenAt)
}
