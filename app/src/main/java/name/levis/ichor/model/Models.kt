package name.levis.ichor.model

import androidx.annotation.StringRes
import kotlinx.serialization.Serializable
import kotlinx.serialization.Transient
import name.levis.ichor.R

// Mirrors the JSON produced by the Go core (go/ichorgo).

@Serializable
data class ConfigSummary(
    val current: String,
    val contexts: List<ContextSummary>,
)

@Serializable
data class ContextSummary(
    val name: String,
    /** Identifies the cluster whatever screenshot mode does to [name]; keys its color. */
    val fingerprint: String = "",
    /** The same for every context of the cluster, on any phone: what share links name. */
    val clusterId: String = "",
    val endpoints: List<String> = emptyList(),
    val nodes: List<String> = emptyList(),
    val roles: List<String> = emptyList(),
    /** The client certificate's expiry, or a kubeconfig token's (JWT `exp`); 0 when unknown. */
    val certNotAfter: Long = 0,
    val demo: Boolean = false,
    /** A Talos context signed in through Sidero Omni (no certificate): who it signs as, and the Omni cluster. */
    val omni: Boolean = false,
    val identity: String = "",
    val cluster: String = "",
    /** [KIND_TALOS] (a talosconfig context) or [KIND_KUBE] (added from a kubeconfig, no Talos API). */
    val kind: String = KIND_TALOS,
    // Kubeconfig contexts only (ParseKubeconfig): what the import preview and the home show.
    val namespace: String = "",
    /** How the context signs in: cert, token, eks, gke, oidc… (the Go core's kubeContextSummary). */
    val auth: String = "",
    /** What the sign-in method signs in to (EKS cluster, OIDC issuer), if anything. */
    val authDetail: String = "",
    /** Who the credentials are: the certificate's common name or the token's subject. */
    val user: String = "",
    val insecure: Boolean = false,
    /** Why the context cannot be added (a kube-* code), "" when it can. */
    val problem: String = "",
    val problemDetail: String = "",
    /** The method the app signs in with (oidc, eks, gke, azure, digitalocean, rancher; omni, omni-service-account for a Talos context through Omni), "" for static credentials. */
    val signIn: String = "",
    /**
     * A Talos cluster's Kubernetes access: the fingerprint of the stored kubeconfig cluster its
     * Kubernetes calls go through, "" for the Talos admin kubeconfig. Set by the app, not the core.
     */
    @Transient val kubeAccess: String = "",
)

const val KIND_TALOS = "talos"
const val KIND_KUBE = "kube"

/** Preserved even when screenshot mode masks the endpoint. */
val ContextSummary.isDemo: Boolean get() = demo

/** Added from a kubeconfig: only the Kubernetes API, no Talos one. */
val ContextSummary.isKube: Boolean get() = kind == KIND_KUBE

/** Features gated by Talos RBAC (rules from Talos v1.14 machined.go). */
enum class Feature(@StringRes val label: Int, val roles: Set<String>) {
    POWER(R.string.common_feature_power, setOf("os:admin", "os:operator")),
    // The server-side health check fetches a Kubernetes admin kubeconfig with the caller's role.
    HEALTH(R.string.common_feature_health, setOf("os:admin")),
    KUBECONFIG(R.string.common_feature_kubeconfig, setOf("os:admin")),
    // The Kubernetes API is reached with the admin kubeconfig Talos only issues to os:admin.
    WORKLOADS(R.string.common_feature_workloads, setOf("os:admin")),
    // DebugService/ContainerRun is admin-only in Talos.
    DEBUG_SHELL(R.string.common_feature_debug_shell, setOf("os:admin")),
    ETCD_DEFRAG(R.string.common_feature_etcd_defrag, setOf("os:admin", "os:operator")),
    // The MachineConfig resource is sensitive in Talos: only os:admin can read it.
    MACHINE_CONFIG(R.string.common_feature_machine_config, setOf("os:admin")),
    ETCD_SNAPSHOT(R.string.common_feature_etcd_snapshot, setOf("os:admin", "os:operator", "os:etcd:backup")),
    // MachineService/ServiceStart|Stop|Restart need os:operator in Talos.
    SERVICE_CONTROL(R.string.common_feature_service_control, setOf("os:admin", "os:operator")),
    // MachineService/GenerateClientConfiguration is admin-only in Talos.
    ISSUE_CONFIG(R.string.common_feature_issue_config, setOf("os:admin")),
    // MachineService/PacketCapture: os:admin and os:operator in Talos.
    PACKET_CAPTURE(R.string.common_feature_packet_capture, setOf("os:admin", "os:operator")),
    // MachineService/Upgrade (and LifecycleService/Upgrade) are admin-only in Talos.
    UPGRADE(R.string.common_feature_upgrade, setOf("os:admin")),
    // MachineService/EtcdForfeitLeadership and EtcdRemoveMemberByID are admin-only in Talos.
    ETCD_MEMBER_ACTIONS(R.string.common_feature_etcd_member_actions, setOf("os:admin")),
    // The cgroup tree is read with MachineService/Copy of /sys/fs/cgroup, admin-only in Talos.
    CGROUPS(R.string.common_feature_cgroups, setOf("os:admin")),
    // Non-sensitive resources are readable by every role; sensitive ones answer "permission denied".
    RESOURCE_BROWSER(R.string.common_feature_resource_browser, setOf("os:admin", "os:operator", "os:reader")),
    // Every role can collect a bundle; what it cannot read (machine config: os:admin, etcd
    // status: os:operator) is left out and noted in the bundle.
    SUPPORT_BUNDLE(R.string.common_feature_support_bundle, setOf("os:admin", "os:operator", "os:reader")),
    ;

    val minimumRole: String
        get() = when {
            "os:reader" in roles -> "os:reader"
            "os:operator" in roles -> "os:operator"
            else -> "os:admin"
        }
}

/**
 * What a cluster added from a kubeconfig can do: its credentials only reach the Kubernetes
 * API, so every feature that goes through Talos is out. What they may do in Kubernetes is
 * the API's call (it answers 403).
 */
private val KUBE_FEATURES = setOf(Feature.WORKLOADS, Feature.KUBECONFIG)

/**
 * A Talos cluster whose Kubernetes access goes through a kubeconfig cluster ([ContextSummary.kubeAccess])
 * reaches Kubernetes with those credentials, whatever its Talos role.
 */
fun ContextSummary.allows(feature: Feature): Boolean = when {
    isKube -> feature in KUBE_FEATURES
    feature == Feature.WORKLOADS && kubeAccess.isNotEmpty() -> true
    // Omni applies the user's own role to every call; it never lets Talos issue credentials.
    omni -> feature !in OMNI_UNAVAILABLE
    else -> roles.any { it in feature.roles }
}

/** What a cluster reached through Omni cannot do: Omni issues its talosconfigs and kubeconfigs. */
private val OMNI_UNAVAILABLE = setOf(Feature.ISSUE_CONFIG, Feature.KUBECONFIG, Feature.WORKLOADS)

/** Short access level for the UI: "admin", "operator" or "read-only"; "Kubernetes" for a kubeconfig cluster. */
val ContextSummary.accessLabel: Int
    @StringRes get() = when {
        isKube -> R.string.common_kind_kubernetes
        omni -> R.string.kube_signin_method_omni
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
    /** Why an unreachable node failed: network, tls, auth or other; null from older cores. */
    val errorKind: String? = null,
    val version: String = "",
    val arch: String = "",
    val platform: String = "",
    val role: String = "unknown",
    val stage: String = "unknown",
    val ready: Boolean = false,
    val unmetConditions: List<UnmetCondition> = emptyList(),
    /** Capacity for the cluster summary; 0 when unknown (older core, or the node did not say). */
    val cpuCount: Int = 0,
    val memTotal: Long = 0,
    val memAvailable: Long = 0,
    /** Internet-facing addresses, IPv4 first; empty when none (or from an older core). */
    val publicIPs: List<String> = emptyList(),
    /**
     * Set by the app, never by the core: when an unreachable node last answered (epoch millis);
     * its hostname, role, version and capacity are then the ones it had (see [withLastKnown]).
     */
    val lastSeen: Long? = null,
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
    /** Set when no control-plane node answered the alarm list: [alarms] is unknown, not empty. */
    val alarmsError: String? = null,
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
    /** Trails [raftIndex] (committed) while the member applies its backlog; 0 from older cores. */
    val raftAppliedIndex: Long = 0,
    val version: String = "",
    val errors: List<String> = emptyList(),
)

@Serializable
data class EtcdAlarm(val memberId: String, val alarm: String)

@Serializable
data class LogTail(
    val lines: List<String> = emptyList(),
    val truncated: Boolean = false,
    /** Structured [lines], same order and count; empty from older cores. */
    val entries: List<LogEntry> = emptyList(),
)

/** Node health as shown in the UI, derived from the Go overview. */
enum class NodeHealth { READY, NOT_READY, UNREACHABLE }

val NodeOverview.health: NodeHealth
    get() = when {
        !reachable -> NodeHealth.UNREACHABLE
        ready -> NodeHealth.READY
        else -> NodeHealth.NOT_READY
    }

/** Worth a full row even in the collapsed nodes card: down, not ready, or reporting a problem. */
val NodeOverview.needsAttention: Boolean
    get() = health != NodeHealth.READY || unmetConditions.isNotEmpty() || !error.isNullOrBlank()

/**
 * The Talos version every answering node runs, which the cluster summary already shows, so the
 * node rows can leave it out; null while they differ or none answered.
 */
fun List<NodeOverview>.sharedVersion(): String? =
    filter { it.reachable }.map { it.version }.distinct().singleOrNull()?.takeIf { it.isNotBlank() }

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
