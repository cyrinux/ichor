package name.levis.talosmobile.model

import kotlinx.serialization.Serializable

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
enum class Feature(val label: String, val roles: Set<String>) {
    POWER("Reboot / shutdown", setOf("os:admin", "os:operator")),
    // The server-side health check fetches a Kubernetes admin kubeconfig with the caller's role.
    HEALTH("Cluster health check", setOf("os:admin")),
    KUBECONFIG("Kubeconfig export", setOf("os:admin")),
    ;

    val minimumRole: String get() = if ("os:operator" in roles) "os:operator" else "os:admin"
}

fun ContextSummary.allows(feature: Feature): Boolean = roles.any { it in feature.roles }

/** Short access level for the UI: "admin", "operator" or "read-only". */
val ContextSummary.accessLabel: String
    get() = when {
        "os:admin" in roles -> "admin"
        "os:operator" in roles -> "operator"
        else -> "read-only"
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
