package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/kube_netpol.go and kube_netpol_read.go.

const val NETPOL_KIND_K8S = "NetworkPolicy"
const val NETPOL_KIND_CILIUM = "CiliumNetworkPolicy"
const val NETPOL_KIND_CILIUM_CLUSTER = "CiliumClusterwideNetworkPolicy"

/** Calico's own NetworkPolicy (projectcalico.org), named apart from the Kubernetes one. */
const val NETPOL_KIND_CALICO = "NetworkPolicy.projectcalico.org"
const val NETPOL_KIND_CALICO_GLOBAL = "GlobalNetworkPolicy"

const val NETPEER_PODS = "pods"
const val NETPEER_NAMESPACES = "namespaces"
const val NETPEER_CIDR = "cidr"
const val NETPEER_ENTITY = "entity"
const val NETPEER_FQDN = "fqdn"
const val NETPEER_SERVICE = "service"
const val NETPEER_NODES = "nodes"

/** The rule matches no peer: Cilium's empty rule `{}`, a deny-all. */
const val NETPEER_NONE = "none"

/** A peer the app does not describe; its value is the Cilium field (toGroups, cidrGroupSelector…) or a Calico negation. */
const val NETPEER_OTHER = "other"

/** A Calico rule that hands the traffic to the next tier or the profiles, which allow it. */
const val NETRULE_PASS = "pass"

/** A Calico rule that only logs the traffic. */
const val NETRULE_LOG = "log"

/** Every network policy of the cluster, and how isolated each namespace is. */
@Serializable
data class NetPolicyReport(
    /** The Cilium policy CRDs are served. */
    val cilium: Boolean = false,
    /** Calico's policy API is served. */
    val calico: Boolean = false,
    val policies: List<NetPolicy> = emptyList(),
    val namespaces: List<NetPolicyNamespace> = emptyList(),
    /** A kind that could not be read. */
    val error: String = "",
)

/** One namespace: its pods, and how many only receive (ingress) or send (egress) what a policy allows. */
@Serializable
data class NetPolicyNamespace(
    val namespace: String,
    val pods: Int = 0,
    val ingressIsolated: Int = 0,
    val egressIsolated: Int = 0,
    /** Namespaced policies, not cluster-wide ones. */
    val policies: Int = 0,
)

@Serializable
data class NetPolicy(
    val kind: String,
    /** Empty for a cluster-wide policy. */
    val namespace: String = "",
    val name: String,
    /** Unix millis. */
    val created: Long = 0,
    /** A selector ("app=db"); empty for every pod (of the namespace for a namespaced policy). */
    val subject: String = "",
    /** A cluster-wide policy pinned to a namespace. */
    val subjectNamespace: String = "",
    /** A Cilium host policy: [subject] selects nodes. */
    val nodes: Boolean = false,
    val description: String = "",
    /** A Calico policy's tier and order: lower orders apply first, in the tier's turn. */
    val tier: String = "",
    val order: Double? = null,
    /** The selected pods are isolated for ingress: only what [ingressRules] allow passes. */
    val ingress: Boolean = false,
    val egress: Boolean = false,
    val ingressRules: List<NetRule> = emptyList(),
    val egressRules: List<NetRule> = emptyList(),
    /** "namespace/name" of the pods it selects now, at most 50 of [podCount]. */
    val pods: List<String> = emptyList(),
    val podCount: Int = 0,
) {
    val key: String get() = "$kind/$namespace/$name"
    val clusterWide: Boolean get() = namespace.isEmpty()

    /** NP, CNP, CCNP, Calico NP or GNP. */
    val kindShort: String
        get() = when (kind) {
            NETPOL_KIND_K8S -> "NP"
            NETPOL_KIND_CILIUM -> "CNP"
            NETPOL_KIND_CILIUM_CLUSTER -> "CCNP"
            NETPOL_KIND_CALICO -> "Calico NP"
            NETPOL_KIND_CALICO_GLOBAL -> "GNP"
            else -> kind
        }

    /** Whose policy it is: Kubernetes, Cilium or Calico, for the search. */
    val family: String
        get() = when (kind) {
            NETPOL_KIND_CILIUM, NETPOL_KIND_CILIUM_CLUSTER -> "Cilium"
            NETPOL_KIND_CALICO, NETPOL_KIND_CALICO_GLOBAL -> "Calico"
            else -> "Kubernetes"
        }

    /** "100", "100.5": the order without a needless fraction; null without one. */
    val orderText: String?
        get() = order?.let { if (it == Math.floor(it)) it.toLong().toString() else it.toString() }

    fun matches(ref: PolicyRef): Boolean = kind == ref.kind && namespace == ref.namespace && name == ref.name
}

/**
 * Traffic from/to any of [peers] on any of [ports] passes, or is denied when [deny]; a Calico
 * rule may instead pass it on ([NETRULE_PASS]) or only log it ([NETRULE_LOG]), see [action].
 */
@Serializable
data class NetRule(
    val deny: Boolean = false,
    val action: String = "",
    /** Empty: any peer. */
    val peers: List<NetPeer> = emptyList(),
    /** Empty: any port. */
    val ports: List<NetPort> = emptyList(),
    /** "HTTP GET /api", "DNS *.example.org". */
    val l7: List<String> = emptyList(),
)

/** One side of a rule; see go/ichorgo/kube_netpol.go for what each [kind] uses. */
@Serializable
data class NetPeer(
    val kind: String,
    /** For pods: empty = the policy's own namespace (any for a cluster-wide one), "*" = any. */
    val namespace: String = "",
    val namespaceSelector: String = "",
    val selector: String = "",
    val value: String = "",
    val except: List<String> = emptyList(),
)

@Serializable
data class NetPort(
    /** TCP, UDP, SCTP, ANY, ICMPv4, ICMPv6. */
    val protocol: String = "",
    /** A number or a named port; empty for every port. */
    val port: String = "",
    val endPort: Int = 0,
)

/** "TCP 5432", "UDP 8000–8100", "TCP" (every port), or null for any protocol on any port. */
val NetPort.label: String?
    get() {
        val proto = protocol.takeUnless { it.isEmpty() || it == "ANY" }
        val number = when {
            port.isEmpty() -> null
            endPort > 0 -> "$port–$endPort"
            else -> port
        }
        return listOfNotNull(proto, number).joinToString(" ").ifEmpty { null }
    }

/** How much of a namespace's pods a direction isolates. */
enum class Isolation { NONE, PARTIAL, FULL }

fun isolation(isolated: Int, pods: Int): Isolation = when {
    isolated <= 0 -> Isolation.NONE
    isolated >= pods -> Isolation.FULL
    else -> Isolation.PARTIAL
}

/**
 * The policies of [namespace] (null for all) whose name, subject or kind contains [query],
 * grouped by namespace (alphabetically), cluster-wide ones last under an empty key.
 */
fun NetPolicyReport.grouped(namespace: String?, query: String): List<Pair<String, List<NetPolicy>>> {
    val q = query.trim()
    return policies
        .filter { namespace == null || it.namespace == namespace || (it.clusterWide && it.subjectNamespace == namespace) }
        .filter { q.isEmpty() || listOf(it.name, it.subject, it.kind, it.kindShort, it.family, it.description).any { f -> f.contains(q, ignoreCase = true) } }
        .groupBy { it.namespace }
        .toList()
        .sortedWith(compareBy({ it.first.isEmpty() }, { it.first }))
        .map { (ns, list) -> ns to list.sortedBy { it.name } }
}

/** The namespaces with policies or pods, for the filter chips. */
val NetPolicyReport.policyNamespaces: List<String>
    get() = (namespaces.map { it.namespace } + policies.map { it.namespace }).filter { it.isNotEmpty() }.distinct().sorted()

fun NetPolicyReport.find(ref: PolicyRef): NetPolicy? = policies.firstOrNull { it.matches(ref) }
