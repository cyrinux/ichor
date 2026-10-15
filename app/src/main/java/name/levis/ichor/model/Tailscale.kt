package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/kube_tailscale.go.

/** The Tailscale Kubernetes operator: its Deployment, connectors, proxy groups, proxy classes and DNS. */
@Serializable
data class Tailscale(
    val installed: Boolean = false,
    /** The served tailscale.com version, "" when not installed. */
    val version: String = "",
    /** A list that failed; what could be read still shows. */
    val error: String = "",
    /** A kind had more objects than the reader keeps. */
    val truncated: Boolean = false,
    /** Null when no operator Deployment was found. */
    val operator: TailscaleOperator? = null,
    /** Problems first in every list. */
    val connectors: List<TailscaleConnector> = emptyList(),
    val proxyGroups: List<TailscaleProxyGroup> = emptyList(),
    val proxyClasses: List<TailscaleProxyClass> = emptyList(),
    val dnsConfig: TailscaleDNSConfig? = null,
    val counts: TailscaleCounts = TailscaleCounts(),
) {
    val isEmpty: Boolean get() = connectors.isEmpty() && proxyGroups.isEmpty() && proxyClasses.isEmpty() && dnsConfig == null

    /** The object screen for a row of [kind]: every Tailscale kind is cluster-scoped. */
    fun ref(kind: TailscaleKind, name: String) =
        KubeObjectRef(TailscaleKind.GROUP, version.ifEmpty { TailscaleKind.DEFAULT_VERSION }, kind.resource, kind.kind, "", name, editable = true)
}

@Serializable
data class TailscaleCounts(val ok: Int = 0, val warning: Int = 0, val critical: Int = 0)

/** The operator's Deployment: when it is down, nothing else is reconciled. */
@Serializable
data class TailscaleOperator(
    val namespace: String = "",
    val name: String = "",
    val ready: Int = 0,
    val desired: Int = 0,
    val image: String = "",
    val health: String = "",
) {
    val serviceHealth: ServiceHealth get() = ServiceHealth.from(health)
}

/** One tailnet device a Connector or ProxyGroup runs. */
@Serializable
data class TailnetDevice(
    val hostname: String = "",
    val tailnetIPs: List<String> = emptyList(),
    val staticEndpoints: List<String> = emptyList(),
)

/** A subnet router, an exit node or an app connector. */
@Serializable
data class TailscaleConnector(
    val name: String = "",
    val hostname: String = "",
    val tailnetIPs: List<String> = emptyList(),
    /** status.subnetRoutes as the operator writes it: comma-separated. */
    val subnetRoutes: String = "",
    val advertisedRoutes: List<String> = emptyList(),
    val exitNode: Boolean = false,
    val appConnector: Boolean = false,
    val appConnectorRoutes: List<String> = emptyList(),
    val tags: List<String> = emptyList(),
    val proxyClass: String = "",
    val replicas: Int = 0,
    val devices: List<TailnetDevice> = emptyList(),
    val ready: Boolean = false,
    val reason: String = "",
    val message: String = "",
    val health: String = "",
) {
    val serviceHealth: ServiceHealth get() = ServiceHealth.from(health)

    /** The routes it advertises: the spec's, else what the status says it serves. */
    val routes: List<String>
        get() = advertisedRoutes.ifEmpty { subnetRoutes.split(',').map { it.trim() }.filter { it.isNotEmpty() } }
}

/** A pool of egress, ingress or kube-apiserver proxies. */
@Serializable
data class TailscaleProxyGroup(
    val name: String = "",
    /** egress, ingress or kube-apiserver. */
    val type: String = "",
    val replicas: Int = 0,
    val proxyClass: String = "",
    val tags: List<String> = emptyList(),
    /** Set for kube-apiserver groups. */
    val url: String = "",
    val devices: List<TailnetDevice> = emptyList(),
    /** At least one proxy runs ([ready]: all of them). */
    val available: Boolean = false,
    val ready: Boolean = false,
    val reason: String = "",
    val message: String = "",
    val health: String = "",
) {
    val serviceHealth: ServiceHealth get() = ServiceHealth.from(health)
}

/** A ProxyClass, and how many Connectors and ProxyGroups name it. */
@Serializable
data class TailscaleProxyClass(
    val name: String = "",
    val usedBy: Int = 0,
    val ready: Boolean = false,
    val reason: String = "",
    val message: String = "",
    val health: String = "",
) {
    val serviceHealth: ServiceHealth get() = ServiceHealth.from(health)
}

/** The nameserver that resolves tailnet names in the cluster. */
@Serializable
data class TailscaleDNSConfig(
    val name: String = "",
    val nameserverIP: String = "",
    val ready: Boolean = false,
    val reason: String = "",
    val message: String = "",
    val health: String = "",
) {
    val serviceHealth: ServiceHealth get() = ServiceHealth.from(health)
}

/** The tailscale.com kinds the screen lists, with the resource the object screen reads. */
enum class TailscaleKind(val resource: String, val kind: String) {
    CONNECTOR("connectors", "Connector"),
    PROXY_GROUP("proxygroups", "ProxyGroup"),
    PROXY_CLASS("proxyclasses", "ProxyClass"),
    DNS_CONFIG("dnsconfigs", "DNSConfig"),
    ;

    companion object {
        const val GROUP = "tailscale.com"
        const val DEFAULT_VERSION = "v1alpha1"
    }
}
