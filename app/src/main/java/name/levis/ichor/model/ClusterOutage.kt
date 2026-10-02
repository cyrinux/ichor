package name.levis.ichor.model

/** Why no node answered, from the core's per-node error kinds. */
enum class OutageCause {
    /** Dial failures or timeouts: most likely the phone is off the cluster's network (VPN, LAN). */
    NETWORK,

    /** The cluster answered but refused this talosconfig (certificate, CA or role). */
    CREDENTIALS,

    /** Anything else, or a mix of causes. */
    OTHER,
}

/** No node answered: the overview shows this once instead of a list of unreachable nodes. */
data class ClusterOutage(
    val cause: OutageCause,
    /** How many nodes were tried. */
    val nodes: Int,
    /** The distinct errors, in node order: usually the same one from every node. */
    val errors: List<String>,
)

/** Null as long as one node answered (or there is none to try). */
val ClusterOverview.outage: ClusterOutage?
    get() {
        if (nodes.isEmpty() || nodes.any { it.reachable }) return null
        val kinds = nodes.map { it.errorKind }.toSet()
        val cause = when {
            kinds == setOf(ERROR_KIND_NETWORK) -> OutageCause.NETWORK
            kinds.isNotEmpty() && kinds.all { it in CREDENTIAL_KINDS } -> OutageCause.CREDENTIALS
            else -> OutageCause.OTHER
        }
        return ClusterOutage(cause, nodes.size, nodes.mapNotNull { it.error?.takeIf(String::isNotBlank) }.distinct())
    }

/** Error kinds set by the Go core (errors.go errorKind). */
private const val ERROR_KIND_NETWORK = "network"
private val CREDENTIAL_KINDS = setOf("tls", "auth")
