package name.levis.ichor.ui.nav

import androidx.annotation.StringRes
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.ListAlt
import androidx.compose.material.icons.outlined.AccountTree
import androidx.compose.material.icons.outlined.Dashboard
import androidx.compose.material.icons.outlined.Dns
import androidx.compose.material.icons.outlined.Inventory2
import androidx.compose.material.icons.outlined.Notifications
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material.icons.outlined.Settings
import androidx.compose.material.icons.outlined.Storage
import androidx.compose.material.icons.outlined.Sync
import androidx.compose.material.icons.outlined.ViewAgenda
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.NavigationRail
import androidx.compose.material3.NavigationRailItem
import androidx.compose.material3.Text
import androidx.compose.material3.VerticalDivider
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.compositionLocalOf
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.navigation.NavHostController
import androidx.navigation.compose.currentBackStackEntryAsState
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.activeSummary
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.Feature
import name.levis.ichor.model.allows
import name.levis.ichor.model.isKube
import name.levis.ichor.model.seedOf
import name.levis.ichor.selectCluster
import name.levis.ichor.model.ClusterLabels
import name.levis.ichor.ui.components.ClusterLogo
import name.levis.ichor.ui.components.rememberClusterLabels
import name.levis.ichor.ui.components.rememberTwoPanes

/**
 * A section of the cluster on screen in the tablet rail, in the iPad sidebar's order (with the
 * nodes after the overview, the rail being Android's only way to them besides the home).
 * [readsKube]: shown only with a role that may use the Kubernetes API.
 */
enum class RailSection(@StringRes val title: Int, val icon: ImageVector, val readsKube: Boolean = false) {
    OVERVIEW(R.string.overview_title, Icons.Outlined.Dashboard),
    NODES(R.string.overview_stat_nodes, Icons.Outlined.Dns),
    ALERTS(R.string.alerts_title, Icons.Outlined.Notifications),
    KUBERNETES(R.string.common_kind_kubernetes, Icons.Outlined.Inventory2, readsKube = true),
    ARGO_CD(R.string.argo_title, Icons.Outlined.Sync, readsKube = true),
    FLUX(R.string.flux_title, Icons.Outlined.AccountTree, readsKube = true),
    DATA_SERVICES(R.string.data_services_title, Icons.Outlined.Storage, readsKube = true),
    ACTIVITY(R.string.activity_title, Icons.AutoMirrored.Outlined.ListAlt),
    SEARCH(R.string.overview_action_search, Icons.Outlined.Search),
    SETTINGS(R.string.settings_title, Icons.Outlined.Settings),
    ;

    /** The screen it opens on [cluster]; null for the overview, the home under every other one. */
    fun route(cluster: ContextSummary): String? = when (this) {
        OVERVIEW -> null
        // Without Talos the nodes are those Kubernetes lists.
        NODES -> if (cluster.isKube) Routes.kubeNodes(null) else Routes.nodes(null)
        ALERTS -> Routes.alerts()
        KUBERNETES -> Routes.workloads()
        ARGO_CD -> Routes.ARGO_CD
        FLUX -> Routes.FLUX
        DATA_SERVICES -> Routes.dataServices()
        ACTIVITY -> Routes.activity(cluster.name)
        SEARCH -> Routes.SEARCH
        SETTINGS -> Routes.SETTINGS
    }
}

/** The sections [cluster] has: those reading the Kubernetes API only with a role that does. */
fun railSections(cluster: ContextSummary): List<RailSection> {
    val kube = cluster.allows(Feature.WORKLOADS)
    return RailSection.entries.filter { kube || !it.readsKube }
}

/** The screens of each section by their route's first word (before any `?` or `/`). */
private val SECTION_ROUTES: Map<String, RailSection> = buildMap {
    put("overview", RailSection.OVERVIEW)
    listOf("nodes", "node", "kube-nodes", "hardware", "network", "nettools", "maintenance", "upgrade", "nodemeta", "kube-node-debug")
        .forEach { put(it, RailSection.NODES) }
    put("alerts", RailSection.ALERTS)
    listOf("workloads", "resources", "resourcelist", "resource", "events", "kube-events", "pod-shell", "storage", "netpol", "policies", "autoscaling", "vms", "vm-console")
        .forEach { put(it, RailSection.KUBERNETES) }
    listOf("argocd", "argocd-app", "argocd-diff", "argocd-windows").forEach { put(it, RailSection.ARGO_CD) }
    listOf("flux", "flux-app", "flux-diff").forEach { put(it, RailSection.FLUX) }
    put("data-services", RailSection.DATA_SERVICES)
    put("activity", RailSection.ACTIVITY)
    put("search", RailSection.SEARCH)
    listOf("settings", "licenses", "funding", "changelog").forEach { put(it, RailSection.SETTINGS) }
}

/**
 * The section a destination [route] (a NavHost route pattern) belongs to; null for a screen of
 * none (the import, a capture...). Read from the route, never the other way round: deep links
 * and notifications navigate before the rail exists.
 */
fun railSelection(route: String?): RailSection? =
    route?.substringBefore('?')?.substringBefore('/')?.let(SECTION_ROUTES::get)

/** True while the tablet rail is shown: the top bar then leaves the cluster switch to it. */
val LocalClusterRail = compositionLocalOf { false }

/**
 * [content] (the NavHost) beside the rail on a window wide enough for two panes, once a cluster
 * is configured; as is otherwise. The content keeps its place in the tree either way, so a
 * resize, a fold or a rotation does not rebuild it.
 */
@Composable
fun WithClusterRail(nav: NavHostController, app: TalosApp, content: @Composable () -> Unit) {
    val config by app.configRepository.config.collectAsStateWithLifecycle()
    val locked by app.appLock.locked.collectAsStateWithLifecycle()
    val entry by nav.currentBackStackEntryAsState()
    val route = entry?.destination?.route
    val stored = config
    val active = stored?.activeSummary
    val shown = rememberTwoPanes() && !locked && active != null && route != null && route != Routes.IMPORT
    Row(Modifier.fillMaxSize()) {
        if (shown && stored != null && active != null) {
            val colors by app.clusterColors.colors.collectAsStateWithLifecycle()
            ClusterRail(
                clusters = stored.summary.contexts,
                current = active,
                colors = colors,
                labels = rememberClusterLabels(),
                selected = railSelection(route),
                onCluster = { context ->
                    val section = railSelection(nav.currentBackStackEntry?.destination?.route)
                    app.selectCluster(context.name)
                    // The same section on the other cluster, from its first screen; the overview when it has none.
                    nav.openSection(section?.takeIf { it in railSections(context) } ?: RailSection.OVERVIEW, context)
                },
                onSection = { nav.openSection(it, active) },
                fleetSelected = route == Routes.FLEET,
                onFleet = {
                    nav.navigate(Routes.FLEET) {
                        popUpTo(Routes.OVERVIEW)
                        launchSingleTop = true
                    }
                },
            )
            VerticalDivider()
        }
        Box(Modifier.weight(1f).fillMaxHeight()) {
            CompositionLocalProvider(LocalClusterRail provides shown) { content() }
        }
    }
}

/** Opens [section] as the only screen over the overview, so rail taps never grow the back stack. */
private fun NavHostController.openSection(section: RailSection, cluster: ContextSummary) {
    val route = section.route(cluster)
    if (route == null) {
        popBackStack(Routes.OVERVIEW, inclusive = false)
        return
    }
    navigate(route) {
        popUpTo(Routes.OVERVIEW)
        launchSingleTop = true
    }
}

/**
 * The tablet rail, as the iPad sidebar: "All clusters" (the fleet view, [fleetSelected] while
 * it is on screen), the clusters (each in its colour, the current one selected), then the
 * sections of the current cluster, the [selected] one highlighted.
 */
@Composable
fun ClusterRail(
    clusters: List<ContextSummary>,
    current: ContextSummary,
    colors: Map<String, Int>,
    labels: ClusterLabels,
    selected: RailSection?,
    onCluster: (ContextSummary) -> Unit,
    onSection: (RailSection) -> Unit,
    fleetSelected: Boolean = false,
    onFleet: () -> Unit = {},
) {
    val clustersLabel = stringResource(R.string.rail_clusters)
    NavigationRail {
        // Many clusters and ten sections outgrow a landscape tablet: the rail scrolls.
        Column(Modifier.verticalScroll(rememberScrollState()).padding(vertical = 8.dp)) {
            NavigationRailItem(
                selected = fleetSelected,
                onClick = onFleet,
                icon = { Icon(Icons.Outlined.ViewAgenda, contentDescription = null) },
                label = { Text(stringResource(R.string.fleet_title), maxLines = 1, overflow = TextOverflow.Ellipsis) },
                modifier = Modifier.width(80.dp),
            )
            Column(Modifier.semantics { contentDescription = clustersLabel }) {
                clusters.forEach { context ->
                    val active = context.name == current.name
                    NavigationRailItem(
                        selected = active,
                        onClick = { if (!active) onCluster(context) },
                        icon = { ClusterLogo(context, Color(colors.seedOf(context)), selected = active, size = 28.dp) },
                        label = { Text(labels.of(context), maxLines = 1, overflow = TextOverflow.Ellipsis) },
                        modifier = Modifier.width(80.dp),
                    )
                }
            }
            HorizontalDivider(Modifier.width(80.dp).padding(horizontal = 16.dp, vertical = 8.dp))
            railSections(current).forEach { section ->
                val title = stringResource(section.title)
                NavigationRailItem(
                    selected = section == selected,
                    onClick = { onSection(section) },
                    icon = { Icon(section.icon, contentDescription = null) },
                    label = { Text(title, maxLines = 1, overflow = TextOverflow.Ellipsis) },
                    modifier = Modifier.width(80.dp),
                )
            }
        }
    }
}
