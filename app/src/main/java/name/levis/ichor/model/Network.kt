package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/network.go and connections.go.

@Serializable
data class NodeNetwork(
    val links: List<LinkInfo> = emptyList(),
    val addresses: List<AddressInfo> = emptyList(),
    val routes: List<RouteInfo> = emptyList(),
    val resolvers: List<String> = emptyList(),
    val timeServers: List<String> = emptyList(),
    /** What the switches announce over LLDP (Talos 1.15+). */
    val neighbors: List<LldpNeighbor> = emptyList(),
    /** False on nodes older than Talos 1.15, which have no LLDP resource. */
    val neighborsSupported: Boolean = false,
    /** Section ("links", "addresses", "routes", "resolvers", "timeServers", "neighbors") -> error. */
    val errors: Map<String, String> = emptyMap(),
)

/** One switch heard on [link] over LLDP. Every text comes from the network: show, never link. */
@Serializable
data class LldpNeighbor(
    val link: String,
    val chassisId: String = "",
    val systemName: String = "",
    val systemDescription: String = "",
    val portId: String = "",
    val portDescription: String = "",
    val managementAddresses: List<String> = emptyList(),
    val vlans: List<LldpVlan> = emptyList(),
)

@Serializable
data class LldpVlan(val id: Int, val name: String = "")

/** The switch's name, else its chassis id. */
val LldpNeighbor.switchName: String get() = systemName.ifEmpty { chassisId }

/** "30 servers", or just "30". */
val LldpVlan.label: String get() = if (name.isEmpty()) id.toString() else "$id $name"

/** The neighbours heard on each link, by link name. */
fun NodeNetwork.neighborsByLink(): Map<String, List<LldpNeighbor>> = neighbors.groupBy { it.link }

/** Whether the Network screen shows the neighbours section: supported and something heard, or a failure. */
val NodeNetwork.showsNeighbors: Boolean
    get() = neighborsSupported && (neighbors.isNotEmpty() || errors[NetworkSection.NEIGHBORS] != null)

@Serializable
data class LinkInfo(
    val name: String,
    /** ether, loopback, … */
    val type: String = "",
    /** bond, vlan, veth, wireguard…; empty for a physical link. */
    val kind: String = "",
    val state: String = "",
    val hardwareAddr: String = "",
    val mtu: Long = 0,
    /** 0 when unknown. */
    val speedMbit: Int = 0,
    /** Pod/CNI plumbing (veth, lxc*, cilium_*…). */
    val virtual: Boolean = false,
)

@Serializable
data class AddressInfo(
    val address: String,
    val link: String = "",
    val family: String = "",
    val scope: String = "",
    val virtual: Boolean = false,
)

@Serializable
data class RouteInfo(
    /** "default" for the default route. */
    val destination: String,
    val gateway: String = "",
    val link: String = "",
    val metric: Long = 0,
    val table: String = "",
    val family: String = "",
    val virtual: Boolean = false,
)

val RouteInfo.isDefault: Boolean get() = destination == "default"

val LinkInfo.isUp: Boolean get() = state.equals("up", ignoreCase = true)

/** The network sections, in the order the Go core names them in [NodeNetwork.errors]. */
object NetworkSection {
    const val LINKS = "links"
    const val ADDRESSES = "addresses"
    const val ROUTES = "routes"
    const val RESOLVERS = "resolvers"
    const val TIME_SERVERS = "timeServers"
    const val NEIGHBORS = "neighbors"
}

/**
 * What the Network screen shows: virtual (CNI/pod) links, and their addresses and routes,
 * only when [showVirtual]. Default routes are always kept.
 */
fun NodeNetwork.visible(showVirtual: Boolean): NodeNetwork = if (showVirtual) this else copy(
    links = links.filterNot { it.virtual },
    addresses = addresses.filterNot { it.virtual },
    routes = routes.filter { it.isDefault || !it.virtual },
)

/** How many items [visible] hides (links + addresses + routes). */
fun NodeNetwork.hiddenVirtualCount(): Int =
    links.count { it.virtual } + addresses.count { it.virtual } + routes.count { it.virtual && !it.isDefault }

@Serializable
data class ConnectionInfo(
    /** tcp, tcp6, udp, udp6. */
    val protocol: String,
    val localIp: String = "",
    val localPort: Long = 0,
    val remoteIp: String = "",
    val remotePort: Long = 0,
    /** LISTEN, ESTABLISHED, … */
    val state: String = "",
    val listening: Boolean = false,
    val pid: Long = 0,
    val processName: String = "",
)

enum class ConnectionFilter { LISTENING, ALL }

private fun ConnectionInfo.matches(query: String): Boolean =
    listOf(protocol, localIp, localPort.toString(), remoteIp, remotePort.toString(), state, processName, pid.toString())
        .any { it.contains(query, ignoreCase = true) } ||
        "$localIp:$localPort".contains(query) || "$remoteIp:$remotePort".contains(query)

/**
 * Connections kept by [filter] whose protocol, address, port, state, process name or pid
 * contains [query] (case-insensitive). The Go core already sorts them (listeners first, by port).
 */
fun List<ConnectionInfo>.filtered(filter: ConnectionFilter, query: String): List<ConnectionInfo> {
    val q = query.trim()
    return filter { (filter == ConnectionFilter.ALL || it.listening) && (q.isEmpty() || it.matches(q)) }
}

/** "10.0.0.1:443", "[fd00::1]:443"; "*" for a wildcard address. */
fun endpoint(ip: String, port: Long): String {
    val host = when {
        ip.isEmpty() || ip == "0.0.0.0" || ip == "::" -> "*"
        ':' in ip -> "[$ip]"
        else -> ip
    }
    return "$host:$port"
}
