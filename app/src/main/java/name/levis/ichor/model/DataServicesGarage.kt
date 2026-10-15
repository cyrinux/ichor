package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo (see DataServices.kt).

@Serializable
data class GarageStatus(
    val error: String = "",
    val instances: List<GarageInstance> = emptyList(),
    /** The garage-operator's declarative layer (garage.rajsingh.info), empty when its CRDs are not served. */
    val version: String = "",
    /** A kind reached the Go core's cap: some entries are not shown. */
    val truncated: Boolean = false,
    val clusters: List<GarageClusterStatus> = emptyList(),
    val buckets: List<GarageBucketStatus> = emptyList(),
    val keys: List<GarageKeyStatus> = emptyList(),
    /** The operator's GarageNode declarations (not the CLI's nodes above). */
    val nodes: List<GarageNodeCr> = emptyList(),
    val operator: GarageOperatorStatus? = null,
) {
    val hasOperator: Boolean get() = version.isNotEmpty()
}

@Serializable
data class GarageInstance(
    val namespace: String = "",
    val name: String = "",
    /** Pod the CLI ran in, "" when none was ready. */
    val pod: String = "",
    val pods: Int = 0,
    val podsReady: Int = 0,
    val version: String = "",
    /** healthy, degraded, unavailable or unknown. */
    val status: String = "",
    /** English, causes first (from the Go core). */
    val message: String = "",
    val connectedNodes: Int = 0,
    val knownNodes: Int = 0,
    val storageNodes: Int = 0,
    val storageNodesUp: Int = 0,
    val partitions: Int = 0,
    val partitionsQuorum: Int = 0,
    val partitionsAllOk: Int = 0,
    /** -1 when unknown. */
    val resyncQueue: Long = -1,
    val resyncErrors: Long = -1,
    val tableSyncQueue: Long = -1,
    val layoutVersion: Long = 0,
    val nodes: List<GarageNode> = emptyList(),
    /** cli-json (full picture) or health (status only). */
    val source: String = "",
) {
    val state: GarageState get() = GarageState.from(status)
    val label: String get() = "$namespace/$name"
    val detailed: Boolean get() = source == "cli-json"
}

@Serializable
data class GarageNode(
    val id: String = "",
    val hostname: String = "",
    val zone: String = "",
    val tags: List<String> = emptyList(),
    /** Kubernetes node of the pod with that hostname, "" when none. */
    val kubeNode: String = "",
    /** Holds a role in the current layout: only those count as down. */
    val storage: Boolean = false,
    val up: Boolean = false,
    /** -1 when up or unknown. */
    val lastSeenSecs: Long = -1,
    val draining: Boolean = false,
    val dataAvail: Long = 0,
    val dataTotal: Long = 0,
    val resyncQueue: Long = -1,
    val resyncErrors: Long = -1,
    val tableSyncQueue: Long = -1,
    val statsError: String = "",
    /** Resync tranquility: 0 resyncs at full speed, 2 is Garage's default, -1 when unknown. */
    val tranquility: Long = -1,
) {
    /** What names the node best: Garage forgets a long-gone node's hostname, its tags often name the host. */
    val label: String get() = hostname.ifEmpty { tags.joinToString(",").ifEmpty { id.take(16) } }
}

enum class GarageState(val wire: String, val health: ServiceHealth) {
    HEALTHY("healthy", ServiceHealth.OK),
    DEGRADED("degraded", ServiceHealth.WARNING),
    UNAVAILABLE("unavailable", ServiceHealth.CRITICAL),
    UNKNOWN("unknown", ServiceHealth.UNKNOWN),
    ;

    companion object {
        fun from(wire: String): GarageState = entries.firstOrNull { it.wire == wire } ?: UNKNOWN
    }
}

/** The garage-operator Deployment's own health. */
@Serializable
data class GarageOperatorStatus(
    val namespace: String = "",
    val name: String = "",
    val ready: Int = 0,
    val desired: Int = 0,
    val health: String = "",
    /** Wire values of [GarageOpReason]. */
    val reasons: List<String> = emptyList(),
) {
    val serviceHealth: ServiceHealth get() = ServiceHealth.from(health)
    val label: String get() = "$namespace/$name"
    val reasonList: List<GarageOpReason> get() = reasons.mapNotNull(GarageOpReason::from)
}

/** One of the operator's conditions the Go core kept: a problem or a transition. */
@Serializable
data class GarageOpCondition(
    val type: String = "",
    val status: String = "",
    val reason: String = "",
    val message: String = "",
    /** Unix ms of lastTransitionTime, 0 when unknown. */
    val time: Long = 0,
)

@Serializable
data class GarageClusterStatus(
    val namespace: String = "",
    val name: String = "",
    /** The garageInstance it drives, "ns/name"; "" when unmatched. */
    val instance: String = "",
    /** Pending..Failed, the operator's own word. */
    val phase: String = "",
    val clusterId: String = "",
    val garageVersion: String = "",
    val replicas: Int = 0,
    val readyReplicas: Int = 0,
    val storageReplicas: Int = 0,
    val storageReadyReplicas: Int = 0,
    val gatewayReplicas: Int = 0,
    val gatewayReadyReplicas: Int = 0,
    val layoutVersion: Long = 0,
    /** 0 when nothing is staged. */
    val stagedLayoutVersion: Long = 0,
    val pendingGatewayTombstones: List<String> = emptyList(),
    val conditions: List<GarageOpCondition> = emptyList(),
    val health: String = "",
    /** Wire values of [GarageOpReason]. */
    val reasons: List<String> = emptyList(),
) {
    val serviceHealth: ServiceHealth get() = ServiceHealth.from(health)
    val label: String get() = "$namespace/$name"
    val reasonList: List<GarageOpReason> get() = reasons.mapNotNull(GarageOpReason::from)
}

@Serializable
data class GarageQuotaUsage(
    val sizeBytes: Long = 0,
    /** 0 = unlimited. */
    val sizeLimit: Long = 0,
    val sizePercent: Int = 0,
    val objectCount: Long = 0,
    /** 0 = unlimited. */
    val objectLimit: Long = 0,
    val objectPercent: Int = 0,
)

@Serializable
data class GarageBucketStatus(
    val namespace: String = "",
    val name: String = "",
    val instance: String = "",
    /** spec.clusterRef.name. */
    val cluster: String = "",
    val phase: String = "",
    val bucketId: String = "",
    /** The operator's own wording. */
    val size: String = "",
    val maxSizeQuota: String = "",
    val quotaUsage: GarageQuotaUsage? = null,
    val websiteEnabled: Boolean = false,
    val websiteUrl: String = "",
    val keysWithAccess: Int = 0,
    val conditions: List<GarageOpCondition> = emptyList(),
    val health: String = "",
    /** Wire values of [GarageOpReason]. */
    val reasons: List<String> = emptyList(),
) {
    val serviceHealth: ServiceHealth get() = ServiceHealth.from(health)
    val label: String get() = "$namespace/$name"
    val reasonList: List<GarageOpReason> get() = reasons.mapNotNull(GarageOpReason::from)
}

@Serializable
data class GarageKeyAllBuckets(
    val read: Boolean = false,
    val write: Boolean = false,
    val owner: Boolean = false,
)

@Serializable
data class GarageKeyPermission(
    val bucket: String = "",
    val read: Boolean = false,
    val write: Boolean = false,
    val owner: Boolean = false,
)

@Serializable
data class GarageKeyStatus(
    val namespace: String = "",
    val name: String = "",
    val instance: String = "",
    val cluster: String = "",
    val phase: String = "",
    /** Garage's internal key id, never the S3 access key. */
    val keyId: String = "",
    val clusterWide: Boolean = false,
    val allBuckets: GarageKeyAllBuckets? = null,
    /** Capped by the Go core; [bucketPermissionsTotal] tells how many more there are. */
    val bucketPermissions: List<GarageKeyPermission> = emptyList(),
    val bucketPermissionsTotal: Int = 0,
    val health: String = "",
    /** Wire values of [GarageOpReason]. */
    val reasons: List<String> = emptyList(),
) {
    val serviceHealth: ServiceHealth get() = ServiceHealth.from(health)
    val label: String get() = "$namespace/$name"
    val reasonList: List<GarageOpReason> get() = reasons.mapNotNull(GarageOpReason::from)
}

/** The operator's GarageNode declaration: one durable Garage identity and layout role. */
@Serializable
data class GarageNodeCr(
    val namespace: String = "",
    val name: String = "",
    val instance: String = "",
    val cluster: String = "",
    val phase: String = "",
    val nodeId: String = "",
    /** The effective zone (spec.zone or zoneFrom). */
    val zone: String = "",
    val capacity: String = "",
    val gateway: Boolean = false,
    val connected: Boolean = false,
    val inLayout: Boolean = false,
    /** Seconds ago, -1 when connected or unknown. */
    val lastSeenSecs: Long = -1,
    val health: String = "",
    /** Wire values of [GarageOpReason]. */
    val reasons: List<String> = emptyList(),
) {
    val serviceHealth: ServiceHealth get() = ServiceHealth.from(health)
    val label: String get() = "$namespace/$name"
    val reasonList: List<GarageOpReason> get() = reasons.mapNotNull(GarageOpReason::from)
}

/** Why a garage-operator item is not ok, as the Go core names it. */
enum class GarageOpReason(val wire: String) {
    CLUSTER_FAILED("clusterFailed"),
    ADMIN_UNREACHABLE("adminUnreachable"),
    OPERATOR_DOWN("operatorDown"),
    DEGRADED("degraded"),
    STORAGE_REPLICAS("storageReplicas"),
    QUORUM_AT_RISK("quorumAtRisk"),
    LAYOUT_STAGED("layoutStaged"),
    TOMBSTONES("tombstones"),
    NODE_DISCONNECTED("nodeDisconnected"),
    NODE_NOT_IN_LAYOUT("nodeNotInLayout"),
    BUCKET_FAILED("bucketFailed"),
    LIFECYCLE("lifecycleNotApplied"),
    LOOKUP_STUCK("lookupStuck"),
    METADATA_DEGRADED("metadataDegraded"),
    DELETION_BLOCKED("deletionBlocked"),
    WEBSITE_EXPOSURE("websiteExposure"),
    KEY_FAILED("keyFailed"),
    ;

    companion object {
        fun from(wire: String): GarageOpReason? = entries.firstOrNull { it.wire == wire }
    }
}
