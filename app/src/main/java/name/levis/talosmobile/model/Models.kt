package name.levis.talosmobile.model

import androidx.annotation.StringRes
import kotlinx.serialization.Serializable
import name.levis.talosmobile.R

// Mirrors the JSON produced by the Go core (go/talosmobile).

@Serializable
data class ConfigSummary(
    val current: String,
    val contexts: List<ContextSummary>,
)

@Serializable
data class ContextSummary(
    val name: String,
    val endpoints: List<String> = emptyList(),
    val nodes: List<String> = emptyList(),
    val roles: List<String> = emptyList(),
    val certNotAfter: Long = 0,
)

/** Features gated by Talos RBAC (rules from Talos v1.14 machined.go). */
enum class Feature(@StringRes val label: Int, val roles: Set<String>) {
    POWER(R.string.common_feature_power, setOf("os:admin", "os:operator")),
    // The server-side health check fetches a Kubernetes admin kubeconfig with the caller's role.
    HEALTH(R.string.common_feature_health, setOf("os:admin")),
    KUBECONFIG(R.string.common_feature_kubeconfig, setOf("os:admin")),
    // DebugService/ContainerRun is admin-only in Talos.
    DEBUG_SHELL(R.string.common_feature_debug_shell, setOf("os:admin")),
    ETCD_DEFRAG(R.string.common_feature_etcd_defrag, setOf("os:admin", "os:operator")),
    ;

    val minimumRole: String get() = if ("os:operator" in roles) "os:operator" else "os:admin"
}

fun ContextSummary.allows(feature: Feature): Boolean = roles.any { it in feature.roles }

/** Short access level for the UI: "admin", "operator" or "read-only". */
val ContextSummary.accessLabel: Int
    @StringRes get() = when {
        "os:admin" in roles -> R.string.common_access_admin
        "os:operator" in roles -> R.string.common_access_operator
        else -> R.string.common_access_read_only
    }

@Serializable
data class ClusterOverview(
    val context: String,
    val nodes: List<NodeOverview>,
)

@Serializable
data class NodeOverview(
    val node: String,
    val hostname: String,
    val reachable: Boolean,
    val error: String? = null,
    val version: String = "",
    val arch: String = "",
    val platform: String = "",
    val role: String = "unknown",
    val stage: String = "unknown",
    val ready: Boolean = false,
    val unmetConditions: List<UnmetCondition> = emptyList(),
)

@Serializable
data class UnmetCondition(val name: String, val reason: String)

@Serializable
data class ServiceInfo(
    val id: String,
    val state: String,
    val health: String,
    val message: String? = null,
    val lastEvent: String? = null,
    val lastChange: Long = 0,
)

@Serializable
data class NodeResources(
    val memTotal: Long,
    val memAvailable: Long,
    val load1: Double,
    val load5: Double,
    val load15: Double,
    val bootTime: Long,
    val cpuCount: Int,
    val cpuModel: String = "",
    val mounts: List<MountUsage> = emptyList(),
)

@Serializable
data class MountUsage(
    val filesystem: String,
    val mountedOn: String,
    val size: Long,
    val available: Long,
)

@Serializable
data class EtcdOverview(
    val error: String? = null,
    val leaderId: String = "",
    val members: List<EtcdMember> = emptyList(),
    val statuses: List<EtcdNodeStatus> = emptyList(),
    val alarms: List<EtcdAlarm> = emptyList(),
)

@Serializable
data class EtcdMember(
    val id: String,
    val hostname: String,
    val peerUrls: List<String> = emptyList(),
    val clientUrls: List<String> = emptyList(),
    val isLearner: Boolean = false,
)

@Serializable
data class EtcdNodeStatus(
    val node: String,
    val error: String? = null,
    val memberId: String = "",
    val isLeader: Boolean = false,
    val isLearner: Boolean = false,
    val dbSize: Long = 0,
    val dbSizeInUse: Long = 0,
    val raftIndex: Long = 0,
    val raftTerm: Long = 0,
    val version: String = "",
    val errors: List<String> = emptyList(),
)

@Serializable
data class EtcdAlarm(val memberId: String, val alarm: String)

@Serializable
data class LogTail(val lines: List<String> = emptyList(), val truncated: Boolean = false)

/** Node health as shown in the UI, derived from the Go overview. */
enum class NodeHealth { READY, NOT_READY, UNREACHABLE }

val NodeOverview.health: NodeHealth
    get() = when {
        !reachable -> NodeHealth.UNREACHABLE
        ready -> NodeHealth.READY
        else -> NodeHealth.NOT_READY
    }

@Serializable
data class KubeSpanOverview(val nodes: List<KubeSpanNode> = emptyList())

@Serializable
data class KubeSpanNode(
    val node: String,
    val error: String? = null,
    val enabled: Boolean = false,
    val up: Int = 0,
    val down: Int = 0,
    val peers: List<KubeSpanPeer> = emptyList(),
)

@Serializable
data class KubeSpanPeer(
    val publicKey: String,
    val label: String,
    val state: String,
    val endpoint: String = "",
    val rx: Long = 0,
    val tx: Long = 0,
    val lastHandshake: Long = 0,
)

/** Space a defragmentation would give back (on-disk size minus space in use). */
val EtcdNodeStatus.reclaimable: Long get() = (dbSize - dbSizeInUse).coerceAtLeast(0)

/**
 * Members to defragment, one at a time as Talos advises: followers first, the leader last
 * (it stays available longest), skipping members that could not be queried.
 */
fun defragOrder(statuses: List<EtcdNodeStatus>): List<EtcdNodeStatus> =
    statuses.filter { it.error == null && it.memberId.isNotEmpty() }
        .sortedWith(compareBy<EtcdNodeStatus> { it.isLeader }.thenByDescending { it.reclaimable })
