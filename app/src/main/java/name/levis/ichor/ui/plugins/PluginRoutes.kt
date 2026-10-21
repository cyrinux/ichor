package name.levis.ichor.ui.plugins

import name.levis.ichor.data.PluginRepository
import name.levis.ichor.model.DataServiceKind
import name.levis.ichor.model.opensAlone
import name.levis.ichor.model.PLUGIN_CATEGORY_DATA_SERVICES
import name.levis.ichor.model.PluginDetectItem
import name.levis.ichor.ui.kubebrowser.KubeBrowserRoutes
import name.levis.ichor.ui.nav.Routes

/**
 * The app screens a plugin may name with `screen: native:<route>`: only these, so a plugin
 * (one day a user's) can never send the app anywhere else. A data service's catalog id
 * ("longhorn") opens its Data services tab.
 */
private val NATIVE_ROUTES: Map<String, String> = mapOf(
    "argocd" to Routes.ARGO_CD,
    "flux" to Routes.FLUX,
    "argo-rollouts" to Routes.ARGO_ROLLOUTS,
    "policies" to Routes.POLICIES,
    "netpol" to Routes.NETWORK_POLICIES,
    "autoscaling" to Routes.AUTOSCALING,
    "vms" to Routes.VIRTUAL_MACHINES,
    "crossplane" to KubeBrowserRoutes.CROSSPLANE,
    "tailscale" to KubeBrowserRoutes.TAILSCALE,
    "cluster-lifecycle" to KubeBrowserRoutes.CLUSTER_LIFECYCLE,
    "load-balancing" to KubeBrowserRoutes.LOAD_BALANCING,
    "storage" to KubeBrowserRoutes.STORAGE,
    "services" to KubeBrowserRoutes.SERVICES,
    "jobs" to KubeBrowserRoutes.JOBS,
    "helm" to KubeBrowserRoutes.HELM,
)

/** The native screen [item] asks for (`screen: native:<route>`), null when none or one the app does not know. */
fun nativePluginRoute(item: PluginDetectItem): String? {
    val name = item.nativeScreen ?: return null
    return NATIVE_ROUTES[name]
        ?: DataServiceKind.entries.firstOrNull { it.catalogId == name }?.let { Routes.dataServices(it, solo = it.opensAlone) }
}

/**
 * Where a plugin's tile leads: its native screen, else its tab in Data services, else its own
 * screen (a plugin of another category, from the Kubernetes home).
 */
fun pluginRoute(item: PluginDetectItem): String = nativePluginRoute(item) ?: when (item.category) {
    PLUGIN_CATEGORY_DATA_SERVICES -> Routes.dataServicesPlugin(item.id)
    else -> KubeBrowserRoutes.plugin(item.id)
}

/**
 * Where plugin [pluginId] leads, as its tile would (a checkup finding, a notification of one):
 * from the plugins detection, else from the catalog's category (no cluster call); null for a
 * plugin this build does not know.
 */
suspend fun pluginRouteFor(plugins: PluginRepository, pluginId: String): String? {
    if (pluginId.isEmpty()) return null
    val detected = runCatching { plugins.detect(hints = "") }.getOrNull()?.items?.firstOrNull { it.id == pluginId }
    if (detected != null) return pluginRoute(detected)
    val known = runCatching { plugins.catalog() }.getOrNull()?.items?.firstOrNull { it.id == pluginId } ?: return null
    return pluginRoute(PluginDetectItem(id = known.id, name = known.name, category = known.category.ifEmpty { PLUGIN_CATEGORY_DATA_SERVICES }))
}
