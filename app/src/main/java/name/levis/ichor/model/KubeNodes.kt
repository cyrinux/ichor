package name.levis.ichor.model

import kotlinx.serialization.Serializable

/**
 * The nodes of a cluster added from a kubeconfig, as the Kubernetes API lists them
 * (Ichorgo.kubeNodes): its home, since there is no Talos overview to read.
 */
@Serializable
data class KubeNodesOverview(
    /** The API server's version, "" when it would not say. */
    val serverVersion: String = "",
    val nodes: List<KubeNodeInfo> = emptyList(),
    /** The credentials may not list nodes (a namespaced ServiceAccount): the rest still works. */
    val forbidden: Boolean = false,
)

/** A node. [cpu] in cores, [memory] in bytes, [created] in Unix seconds. */
@Serializable
data class KubeNodeInfo(
    val name: String,
    val roles: List<String> = emptyList(),
    val ready: Boolean = false,
    val cordoned: Boolean = false,
    val internalIP: String = "",
    val externalIP: String = "",
    val kubelet: String = "",
    val osImage: String = "",
    val kernel: String = "",
    val runtime: String = "",
    val arch: String = "",
    /** The autoscaler pool the node came from; [poolKind] is "karpenter", "eks", "gke" or "aks". */
    val pool: String = "",
    val poolKind: String = "",
    /** The cloud machine type (node.kubernetes.io/instance-type). */
    val instanceType: String = "",
    /** "spot", "on-demand" or "reserved" when the cloud labels say which. */
    val capacity: String = "",
    val cpu: Double = 0.0,
    val memory: Double = 0.0,
    val podLimit: Int = 0,
    /** The problem conditions that are on: MemoryPressure, DiskPressure, PIDPressure, NetworkUnavailable. */
    val pressure: List<String> = emptyList(),
    val created: Long = 0,
)

/** The address to show for [KubeNodeInfo]: the internal one, else the external one. */
val KubeNodeInfo.address: String get() = internalIP.ifEmpty { externalIP }

/** Ready and free of pressure: nothing to look at. */
val KubeNodeInfo.healthy: Boolean get() = ready && pressure.isEmpty()

val KubeNodesOverview.readyCount: Int get() = nodes.count { it.ready }

/** The cluster at a glance, as the Talos overview grades it: every node ready, none, or some. */
val KubeNodesOverview.status: ClusterStatus
    get() = when (readyCount) {
        nodes.size -> ClusterStatus.HEALTHY
        0 -> ClusterStatus.DOWN
        else -> ClusterStatus.DEGRADED
    }

/** The names of the nodes that are not ready: the likely cause of a data service's problems. */
fun KubeNodesOverview.notReadyNames(): Set<String> = nodes.filter { !it.ready }.map { it.name }.toSet()

/** Hostnames of the nodes Talos reports not ready or unreachable: candidates for a likely cause. */
fun ClusterOverview.downHostnames(): Set<String> =
    nodes.filter { it.health != NodeHealth.READY }.map { it.hostname }.filter { it.isNotEmpty() }.toSet()
