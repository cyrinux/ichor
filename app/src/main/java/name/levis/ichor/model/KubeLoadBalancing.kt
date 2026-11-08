package name.levis.ichor.model

import kotlinx.serialization.Serializable

/**
 * The Load balancing screen (Go `KubeLoadBalancing`): the controllers that give LoadBalancer
 * Services their address (MetalLB, Cilium's LB IPAM, kube-vip), their pools, MetalLB's
 * advertisements and BGP peers, kube-vip's ranges and leases, and every LoadBalancer Service
 * with why it waits. [controller] takes a Service without class, "" when none was found.
 */
@Serializable
data class KubeLoadBalancing(
    val controller: String = "",
    val controllers: List<String> = emptyList(),
    val pools: List<LbPool> = emptyList(),
    val advertisements: List<LbAdvertisement> = emptyList(),
    val bgpPeers: List<LbBgpPeer> = emptyList(),
    val configErrors: List<LbConfigError> = emptyList(),
    val services: List<LbService> = emptyList(),
    val kubeVip: LbKubeVip? = null,
    val partialAccess: Boolean = false,
) {
    val pending: List<LbService> get() = services.filter { it.pending }
    val empty: Boolean get() = controllers.isEmpty() && services.isEmpty()
}

/** A pool: [available] of [total] addresses left ([countsKnown]: from the pool's status). */
@Serializable
data class LbPool(
    val name: String = "",
    val controller: String = "",
    val addresses: List<String> = emptyList(),
    val total: Int = 0,
    val assigned: Int = 0,
    val available: Int = 0,
    val countsKnown: Boolean = false,
    val autoAssign: Boolean = true,
    val namespaces: List<String> = emptyList(),
    val priority: Int = 0,
    /** "l2:name" or "bgp:name"; empty when nothing advertises the pool (MetalLB). */
    val advertisedBy: List<String> = emptyList(),
    val services: List<String> = emptyList(),
    @Serializable(with = StorageLevelSerializer::class)
    val level: StorageLevel = StorageLevel.OK,
) {
    /** Share of the pool in use, 0 to 1. */
    val used: Float get() = if (total > 0) (total - available).coerceAtLeast(0).toFloat() / total else 0f
}

@Serializable
data class LbAdvertisement(
    val name: String = "",
    /** metallb, or cilium for a CiliumL2AnnouncementPolicy. */
    val controller: String = "",
    /** Why a Cilium policy announces nothing: l2Disabled, noLoadBalancerIPs, noService; empty when it works. */
    val inactive: String = "",
    /** l2 or bgp. */
    val kind: String = "",
    val pools: List<String> = emptyList(),
    val allPools: Boolean = false,
    val nodeSelectors: Int = 0,
    val interfaces: List<String> = emptyList(),
    val peers: List<String> = emptyList(),
    val communities: List<String> = emptyList(),
    val localPref: Int = 0,
    val aggregationLength: Int = 0,
)

/** A BGP peer; [sessionsKnown] false when no speaker's metrics could be read (never "down" then). */
@Serializable
data class LbBgpPeer(
    val name: String = "",
    val peerAddress: String = "",
    val `interface`: String = "",
    val peerASN: Long = 0,
    val myASN: Long = 0,
    val dynamicASN: String = "",
    val nodeSelectors: Int = 0,
    val bfdProfile: String = "",
    val ebgpMultiHop: Boolean = false,
    val holdTime: String = "",
    val sessions: List<LbBgpSession> = emptyList(),
    val sessionsKnown: Boolean = false,
    @Serializable(with = StorageLevelSerializer::class)
    val level: StorageLevel = StorageLevel.OK,
)

@Serializable
data class LbBgpSession(val node: String = "", val up: Boolean = false)

@Serializable
data class LbConfigError(val component: String = "", val node: String = "", val message: String = "")

/** A LoadBalancer Service: its addresses, pool, announcing node and, while it waits, [cause]. */
@Serializable
data class LbService(
    val namespace: String = "",
    val name: String = "",
    val `class`: String = "",
    val addresses: List<String> = emptyList(),
    val pool: String = "",
    val pending: Boolean = false,
    val cause: String = "",
    val announcedBy: String = "",
    val interfaces: List<String> = emptyList(),
    val peers: List<String> = emptyList(),
    val since: Long = 0,
    @Serializable(with = StorageLevelSerializer::class)
    val level: StorageLevel = StorageLevel.OK,
) {
    val key: String get() = "$namespace/$name"
}

@Serializable
data class LbKubeVip(val ranges: List<LbKubeVipRange> = emptyList(), val leases: List<LbKubeVipLease> = emptyList())

/** kube-vip's addresses for [scope] ("global" or a namespace). */
@Serializable
data class LbKubeVipRange(val scope: String = "", val cidrs: List<String> = emptyList())

@Serializable
data class LbKubeVipLease(val name: String = "", val namespace: String = "", val holder: String = "", val renewed: Long = 0)

/** Why a LoadBalancer Service has no address, as the core names it; [UNKNOWN] for a cause this version does not know. */
enum class LbCause(val wire: String) {
    NO_CONTROLLER("noController"),
    CONFIG_ERROR("configError"),
    CONTROLLER_DOWN("controllerDown"),
    NO_POOL("noPool"),
    POOL_NOT_MATCHED("poolNotMatched"),
    POOL_EXHAUSTED("poolExhausted"),
    POOL_NOT_ADVERTISED("poolNotAdvertised"),
    UNKNOWN("unknown"),
    ;

    companion object {
        fun of(wire: String): LbCause = entries.firstOrNull { it.wire == wire } ?: UNKNOWN

        /** The cause a checkup finding's reason names, null when it names none. */
        fun ofReason(reason: String): LbCause? = entries.firstOrNull { it.wire == reason }
    }
}
