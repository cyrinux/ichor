package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/talosmobile/kube_routes.go.

@Serializable
data class KubeRouteList(val routes: List<KubeRoute> = emptyList())

/** A URL an app is served at: a host of an Ingress or HTTPRoute whose backend selects its pods. */
@Serializable
data class KubeRoute(
    /** Ingress or HTTPRoute. */
    val kind: String,
    val namespace: String,
    val name: String,
    val url: String,
    /** The backend Service, in [namespace]. */
    val service: String = "",
) {
    /** The URL without its scheme, to show. */
    val label: String get() = url.substringAfter("://")
}

/** One pod of an app, as KubeAppRoutes takes it. */
@Serializable
data class RoutePod(val namespace: String, val pod: String)

val InventoryApp.routePods: List<RoutePod>
    get() = pods.map { RoutePod(it.namespace, it.pod) }.distinct()
