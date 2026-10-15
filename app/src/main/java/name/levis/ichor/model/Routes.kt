package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/kube_routes.go.

@Serializable
data class KubeRouteList(val routes: List<KubeRoute> = emptyList())

/**
 * A URL an app is served at: a host of an Ingress, a Gateway API HTTPRoute, GRPCRoute or
 * TLSRoute, or a Traefik IngressRoute(TCP) whose backend selects its pods.
 */
@Serializable
data class KubeRoute(
    /** Ingress, HTTPRoute, GRPCRoute, TLSRoute, IngressRoute or IngressRouteTCP, shown as is. */
    val kind: String,
    val namespace: String,
    val name: String,
    val url: String,
    /** The backend Service, in [namespace]. */
    val service: String = "",
    /** The API group the kind is read at ("" for the core group). */
    val group: String = "",
    val version: String = "",
    val resource: String = "",
) {
    /** The URL without its scheme, to show. */
    val label: String get() = url.substringAfter("://")

    /**
     * Where the route itself is read, so its object summary opens: null when the cluster's
     * answer does not carry the resource (an older ichorgo).
     */
    val objectRef: KubeObjectRef?
        get() = if (resource.isEmpty()) null else KubeObjectRef(group, version, resource, kind, namespace, name, editable = false)
}

/** One pod of an app, as KubeAppRoutes takes it. */
@Serializable
data class RoutePod(val namespace: String, val pod: String)

val InventoryApp.routePods: List<RoutePod>
    get() = pods.map { RoutePod(it.namespace, it.pod) }.distinct()
