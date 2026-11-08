package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/kube_clusterapi.go.

/**
 * A management cluster's Cluster API state (and Sidero Metal's): the workload clusters, their
 * machines and machine deployments, and the bare-metal servers, problems first in every list.
 */
@Serializable
data class ClusterLifecycle(
    /** The cluster serves cluster.x-k8s.io or metal.sidero.dev. */
    val installed: Boolean = false,
    /** The served cluster.x-k8s.io version, "" without Cluster API. */
    val version: String = "",
    val sidero: Boolean = false,
    val talosControlPlane: Boolean = false,
    val talosBootstrap: Boolean = false,
    /** A list that failed; what could be read still shows. */
    val error: String = "",
    /** A kind had more objects than the reader keeps. */
    val truncated: Boolean = false,
    val clusters: List<CapiCluster> = emptyList(),
    val machines: List<CapiMachine> = emptyList(),
    val machineDeployments: List<CapiMachineDeployment> = emptyList(),
    val servers: List<SideroServer> = emptyList(),
    val serverClasses: List<SideroServerClass> = emptyList(),
    val counts: ClusterLifecycleCounts = ClusterLifecycleCounts(),
) {
    val isEmpty: Boolean get() = clusters.isEmpty() && machines.isEmpty() && machineDeployments.isEmpty() && servers.isEmpty() && serverClasses.isEmpty()

    /** The object screen for a row of [kind]: Cluster API kinds are namespaced, Sidero's cluster-scoped. */
    fun ref(kind: LifecycleKind, namespace: String, name: String): KubeObjectRef {
        val version = if (kind.group == CAPI_GROUP) version.ifEmpty { CAPI_DEFAULT_VERSION } else SIDERO_VERSION
        return KubeObjectRef(kind.group, version, kind.resource, kind.kind, if (kind.namespaced) namespace else "", name, editable = true)
    }

    /**
     * The machines by cluster (its namespace too: two clusters may share a name), the clusters in
     * the order their first machine comes (problems first, as the core sorted them), each
     * cluster's control plane first.
     */
    val machinesByCluster: List<Pair<String, List<CapiMachine>>>
        get() = machines.groupBy { "${it.namespace}/${it.cluster}" }.values.map { list -> list.first().cluster to list.sortedByDescending { it.controlPlane } }
}

@Serializable
data class ClusterLifecycleCounts(
    val clusters: Int = 0,
    val machines: Int = 0,
    val servers: Int = 0,
    val ok: Int = 0,
    val warning: Int = 0,
    val critical: Int = 0,
)

@Serializable
data class CapiReplicas(val desired: Int = 0, val ready: Int = 0, val available: Int = 0, val upToDate: Int = 0)

/** A condition that is not True. */
@Serializable
data class CapiCondition(val type: String = "", val status: String = "", val reason: String = "", val message: String = "")

/** Why a row needs attention: its failure first, else its conditions that are not True. */
private fun capiProblem(failureReason: String, failureMessage: String, conditions: List<CapiCondition>): String =
    listOf(failureReason, failureMessage).filter { it.isNotEmpty() }.joinToString(": ").ifEmpty {
        conditions.joinToString("; ") { c -> listOf(c.type, c.message.ifEmpty { c.reason }).filter { it.isNotEmpty() }.joinToString(": ") }
    }

/** A workload cluster. */
@Serializable
data class CapiCluster(
    val namespace: String = "",
    val name: String = "",
    val phase: String = "",
    val paused: Boolean = false,
    /** host:port, "" when unset. */
    val endpoint: String = "",
    val infraKind: String = "",
    val controlPlaneKind: String = "",
    val infrastructureReady: Boolean = false,
    val controlPlaneReady: Boolean = false,
    /** Null when the control plane provider reports no replicas. */
    val controlPlane: CapiReplicas? = null,
    val workers: CapiReplicas = CapiReplicas(),
    val versions: List<String> = emptyList(),
    val conditions: List<CapiCondition> = emptyList(),
    val failureReason: String = "",
    val failureMessage: String = "",
    val health: String = "",
    val reasons: List<String> = emptyList(),
) {
    val serviceHealth: ServiceHealth get() = ServiceHealth.from(health)
    val problem: String get() = capiProblem(failureReason, failureMessage, conditions)
}

/** A Machine: a node of a workload cluster, from request to join. */
@Serializable
data class CapiMachine(
    val namespace: String = "",
    val name: String = "",
    val cluster: String = "",
    val controlPlane: Boolean = false,
    val deployment: String = "",
    val phase: String = "",
    /** "" while not joined. */
    val node: String = "",
    val version: String = "",
    val providerId: String = "",
    val addresses: List<String> = emptyList(),
    val bootstrapReady: Boolean = false,
    val infrastructureReady: Boolean = false,
    /** Unix ms, 0 when never. */
    val lastUpdated: Long = 0,
    val createdAt: Long = 0,
    /** Not moving for over half an hour in a transient phase. */
    val stuck: Boolean = false,
    val failureReason: String = "",
    val failureMessage: String = "",
    val conditions: List<CapiCondition> = emptyList(),
    val health: String = "",
    val reasons: List<String> = emptyList(),
) {
    val serviceHealth: ServiceHealth get() = ServiceHealth.from(health)
    val problem: String get() = capiProblem(failureReason, failureMessage, conditions)

    /** When it last moved, as far as the report says: its last update, else its creation; 0 unknown. */
    val stuckSince: Long get() = lastUpdated.takeIf { it > 0 } ?: createdAt
}

@Serializable
data class CapiMachineDeployment(
    val namespace: String = "",
    val name: String = "",
    val cluster: String = "",
    val phase: String = "",
    val desired: Int = 0,
    val ready: Int = 0,
    val available: Int = 0,
    val upToDate: Int = 0,
    val unavailable: Int = 0,
    val version: String = "",
    val health: String = "",
    val reasons: List<String> = emptyList(),
) {
    val serviceHealth: ServiceHealth get() = ServiceHealth.from(health)
}

/** A Sidero Metal bare-metal server. */
@Serializable
data class SideroServer(
    val name: String = "",
    val hostname: String = "",
    val accepted: Boolean = false,
    val cordoned: Boolean = false,
    val ready: Boolean = false,
    val inUse: Boolean = false,
    val isClean: Boolean = false,
    /** on, off or unknown. */
    val power: String = "",
    val addresses: List<String> = emptyList(),
    val serverClasses: List<String> = emptyList(),
    val cpu: String = "",
    val memoryBytes: Long = 0,
    val health: String = "",
    val reasons: List<String> = emptyList(),
) {
    val serviceHealth: ServiceHealth get() = ServiceHealth.from(health)
}

/** A Sidero ServerClass and how many of its servers are free or taken. */
@Serializable
data class SideroServerClass(val name: String = "", val available: Int = 0, val inUse: Int = 0)

/** Cluster API's core group and Sidero Metal's, and the versions read when the report names none. */
const val CAPI_GROUP = "cluster.x-k8s.io"
const val SIDERO_GROUP = "metal.sidero.dev"
const val CAPI_DEFAULT_VERSION = "v1beta1"
const val SIDERO_VERSION = "v1alpha2"

/** The kinds the screen lists, with what the object screen reads. */
enum class LifecycleKind(val group: String, val resource: String, val kind: String, val namespaced: Boolean) {
    CLUSTER(CAPI_GROUP, "clusters", "Cluster", true),
    MACHINE(CAPI_GROUP, "machines", "Machine", true),
    MACHINE_DEPLOYMENT(CAPI_GROUP, "machinedeployments", "MachineDeployment", true),
    SERVER(SIDERO_GROUP, "servers", "Server", false),
    SERVER_CLASS(SIDERO_GROUP, "serverclasses", "ServerClass", false),
}
