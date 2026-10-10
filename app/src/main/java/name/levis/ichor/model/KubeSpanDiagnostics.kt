package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/kubespan_diag.go (KubeSpanDiagnosticsAll).

@Serializable
data class KubeSpanDiagAll(val nodes: List<KubeSpanDiag> = emptyList())

@Serializable
data class KubeSpanDiag(
    val node: String = "",
    val hostname: String = "",
    val config: KubeSpanDiagConfig? = null,
    /** The kubespan link's MTU, 0 when the link is not there. */
    val linkMtu: Long = 0,
    val peers: List<KubeSpanDiagPeer> = emptyList(),
    /** Set on a node Omni manages. */
    val siderolink: SiderolinkDiag? = null,
    /** Sections that could not be read, by name. */
    val errors: Map<String, String> = emptyMap(),
)

@Serializable
data class KubeSpanDiagConfig(
    val enabled: Boolean = false,
    /** 0: Talos's default. */
    val mtu: Long = 0,
    val forceRouting: Boolean = false,
    val advertiseKubernetesNetworks: Boolean = false,
    val endpointFilters: List<String> = emptyList(),
    val harvestExtraEndpoints: Boolean = false,
)

@Serializable
data class KubeSpanDiagPeer(
    val publicKey: String = "",
    val label: String = "",
    val state: String = "",
    val address: String = "",
    val allowedIPs: List<String> = emptyList(),
    val endpointsTried: List<String> = emptyList(),
    val endpoint: String = "",
    val lastUsedEndpoint: String = "",
    /** Unix seconds, 0 = never. */
    val lastHandshake: Long = 0,
    val lastEndpointChange: Long = 0,
    val rx: Long = 0,
    val tx: Long = 0,
    /** Why the link is not right, in the core's words; empty when nothing was found. */
    val verdicts: List<KubeSpanVerdict> = emptyList(),
)

@Serializable
data class KubeSpanVerdict(val kind: String = "", val message: String = "")

@Serializable
data class SiderolinkDiag(
    val host: String = "",
    val connected: Boolean = false,
    val linkName: String = "",
    val grpcTunnel: Boolean = false,
    val nodeAddress: String = "",
    val mtu: Int = 0,
)

/** [node]'s diagnosis, null when it was not read. */
fun KubeSpanDiagAll.node(node: String): KubeSpanDiag? = nodes.firstOrNull { it.node == node }

/** The peer [publicKey] as [node] sees it. */
fun KubeSpanDiagAll.peer(node: String, publicKey: String): KubeSpanDiagPeer? = node(node)?.peers?.firstOrNull { it.publicKey == publicKey }
