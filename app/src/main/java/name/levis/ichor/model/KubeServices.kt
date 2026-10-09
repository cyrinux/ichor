package name.levis.ichor.model

import kotlinx.serialization.Serializable

/**
 * The Services screen (Go `KubeServices`): each Service with its addresses, ready endpoints
 * and routes, problems first. [partialAccess]: EndpointSlices or routes could not be read.
 * [lbController] names what should give a LoadBalancer Service its address (metallb, cilium),
 * set only when one waits for it.
 */
@Serializable
data class KubeServices(
    val services: List<ServiceRow> = emptyList(),
    val partialAccess: Boolean = false,
    val lbController: String = "",
) {
    val anyPending: Boolean get() = services.any { it.pending }
}

/**
 * A Service. [ports]: kubectl's "80/TCP" ("80:30080/TCP" with a node port). [addresses]: the
 * load balancer's IPs or hostnames and the external IPs. [endpointsKnown]: the slices were read.
 */
@Serializable
data class ServiceRow(
    val namespace: String = "",
    val name: String = "",
    val type: String = "",
    val clusterIP: String = "",
    val externalName: String = "",
    val ports: List<String> = emptyList(),
    val addresses: List<String> = emptyList(),
    val lbClass: String = "",
    val pending: Boolean = false,
    val selector: Boolean = false,
    val endpointsKnown: Boolean = false,
    val endpoints: Int = 0,
    val readyEndpoints: Int = 0,
    val routes: List<KubeRoute> = emptyList(),
    /** Read as a [StorageLevel]: the screens share ok, warning and critical. */
    @Serializable(with = StorageLevelSerializer::class)
    val level: StorageLevel = StorageLevel.OK,
) {
    val key: String get() = "$namespace/$name"

    /** A Service without a cluster IP: its DNS name lists the pods instead. */
    val headless: Boolean get() = clusterIP.equals("None", ignoreCase = true)

    /** "2/3", when the endpoints are worth showing: known, and kept by Kubernetes or present. */
    val readyText: String? get() = "$readyEndpoints/$endpoints".takeIf { endpointsKnown && (selector || endpoints > 0) }
}

/** What gives LoadBalancer Services their address, as the core names it. */
enum class LbController(val wire: String) {
    METALLB("metallb"),
    CILIUM("cilium"),
    ;

    companion object {
        fun of(wire: String): LbController? = entries.firstOrNull { it.wire == wire }
    }
}

/** The Services whose namespace/name, type, an address, a port or a route URL contains [query] (any case). */
fun List<ServiceRow>.filteredServices(query: String): List<ServiceRow> {
    val q = query.trim()
    if (q.isEmpty()) return this
    return filter { s ->
        listOf(s.key, s.type, s.clusterIP, s.externalName).any { it.contains(q, ignoreCase = true) } ||
            (s.addresses + s.ports + s.routes.map { it.url }).any { it.contains(q, ignoreCase = true) }
    }
}
