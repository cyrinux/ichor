package name.levis.ichor.model

import kotlinx.serialization.Serializable
import java.util.Locale

// Mirrors go/ichorgo/kube_cilium.go, kube_hubble_agg.go and kube_hubble_flow.go.

const val HUBBLE_FORWARDED = "FORWARDED"
const val HUBBLE_DROPPED = "DROPPED"
const val HUBBLE_AUDIT = "AUDIT"
const val HUBBLE_ERROR = "ERROR"

const val HUBBLE_INGRESS = "INGRESS"
const val HUBBLE_EGRESS = "EGRESS"

const val HUBBLE_NODE_CONNECTING = "connecting"
const val HUBBLE_NODE_LIVE = "live"
const val HUBBLE_NODE_ERROR = "error"

/** Whether Cilium runs on the cluster, and with Hubble. */
@Serializable
data class CiliumStatus(
    val installed: Boolean = false,
    val namespace: String = "",
    /** The agents' image tag. */
    val version: String = "",
    val hubble: Boolean = false,
    /** Flows each agent keeps. */
    val buffer: Int = 0,
    val agents: List<CiliumAgent> = emptyList(),
)

@Serializable
data class CiliumAgent(val node: String = "", val pod: String = "", val ready: Boolean = false)

/** What the flow stream has seen so far; a new one at most every second. */
@Serializable
data class HubbleSnapshot(
    val namespace: String = "",
    val version: String = "",
    val buffer: Int = 0,
    val nodes: List<HubbleNodeState> = emptyList(),
    /** Newest first. */
    val flows: List<HubbleFlow> = emptyList(),
    /** Last seen first. */
    val drops: List<DropGroup> = emptyList(),
    val seen: Long = 0,
    val dropped: Long = 0,
    /** Events Hubble lost (its ring buffer overran). */
    val lost: Long = 0,
    /** The policies could not be read: drops are not attributed. */
    val policiesError: String = "",
)

@Serializable
data class HubbleNodeState(
    val node: String = "",
    val pod: String = "",
    /** [HUBBLE_NODE_CONNECTING], [HUBBLE_NODE_LIVE] or [HUBBLE_NODE_ERROR]. */
    val state: String = "",
    val error: String = "",
    val flows: Long = 0,
)

@Serializable
data class HubbleFlow(
    /** Unix millis. */
    val time: Long = 0,
    val node: String = "",
    val verdict: String = "",
    /** Drop reason (POLICY_DENIED…). */
    val reason: String = "",
    val direction: String = "",
    val protocol: String = "",
    /** Destination port. */
    val port: Int = 0,
    /** TCP flags, "SYN,ACK". */
    val flags: String = "",
    val reply: Boolean = false,
    val type: String = "",
    /** "DNS query example.org. A", "HTTP GET /". */
    val l7: String = "",
    val source: HubblePeer = HubblePeer(),
    val destination: HubblePeer = HubblePeer(),
    val deniedBy: List<PolicyRef> = emptyList(),
)

@Serializable
data class HubblePeer(
    val namespace: String = "",
    val pod: String = "",
    val workload: String = "",
    val identity: Long = 0,
    val ip: String = "",
    /** DNS names Cilium knows for the IP. */
    val names: List<String> = emptyList(),
    /** world, host, remote-node, kube-apiserver… */
    val reserved: String = "",
)

/** The drops between two endpoints on one port, counted together. */
@Serializable
data class DropGroup(
    val source: HubblePeer = HubblePeer(),
    val destination: HubblePeer = HubblePeer(),
    val protocol: String = "",
    val port: Int = 0,
    val direction: String = "",
    /** [HUBBLE_DROPPED], or [HUBBLE_AUDIT]: it would have been. */
    val verdict: String = "",
    val reason: String = "",
    val count: Long = 0,
    val firstSeen: Long = 0,
    val lastSeen: Long = 0,
    val nodes: List<String> = emptyList(),
    /** Explicit deny rules the flow names. */
    val deniedBy: List<PolicyRef> = emptyList(),
    /** Policies that put the endpoint in default-deny. */
    val isolating: List<PolicyRef> = emptyList(),
    val sample: HubbleFlow = HubbleFlow(),
) {
    val key: String get() = "${source.label}|${destination.label}|$protocol|$port|$direction|$verdict|$reason"
}

@Serializable
data class PolicyRef(val kind: String = "", val namespace: String = "", val name: String = "") {
    val label: String get() = if (namespace.isEmpty()) name else "$namespace/$name"
}

/** "namespace/pod", else the reserved identity with its DNS name or IP, else the IP. */
val HubblePeer.label: String
    get() = when {
        pod.isNotEmpty() -> if (namespace.isEmpty()) pod else "$namespace/$pod"
        workload.isNotEmpty() -> if (namespace.isEmpty()) workload else "$namespace/$workload"
        else -> listOfNotNull(reserved.ifEmpty { null }, (names.firstOrNull() ?: ip).ifEmpty { null })
            .joinToString(" ")
            .ifEmpty { "?" }
    }

/** "TCP 5432", "UDP", "ICMPv4"; empty when unknown. */
fun portLabel(protocol: String, port: Int): String = listOfNotNull(protocol.ifEmpty { null }, port.takeIf { it > 0 }?.toString()).joinToString(" ")

/** "STALE_OR_UNROUTABLE_IP" → "Stale or unroutable IP", for a reason without its own sentence. */
fun humanizeReason(reason: String): String {
    val words = reason.split('_').filter { it.isNotEmpty() }.map { w ->
        if (w in KEEP_UPPER) w else w.lowercase(Locale.ROOT)
    }
    return words.joinToString(" ").replaceFirstChar { it.titlecase(Locale.ROOT) }
}

private val KEEP_UPPER = setOf("IP", "IPV4", "IPV6", "CT", "TCP", "UDP", "ICMP", "DNS", "L3", "L4", "L7", "NAT", "MTU", "TTL", "VLAN", "BPF", "IPSEC", "SCTP")

/** The namespaces of the pods in [snapshot], to pick a filter from. */
fun HubbleSnapshot.namespaces(): Set<String> =
    (flows.flatMap { listOf(it.source, it.destination) } + drops.flatMap { listOf(it.source, it.destination) })
        .map { it.namespace }
        .filter { it.isNotEmpty() }
        .toSet()

/** What the flow stream shows: one namespace (null for all), one pod of it, only drops. */
data class HubbleFilter(val namespace: String? = null, val pod: String? = null, val dropsOnly: Boolean = false)
