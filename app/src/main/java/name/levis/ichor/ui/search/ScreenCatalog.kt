package name.levis.ichor.ui.search

import androidx.annotation.StringRes
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.ListAlt
import androidx.compose.material.icons.outlined.AccountTree
import androidx.compose.material.icons.outlined.AddCircleOutline
import androidx.compose.material.icons.outlined.Checklist
import androidx.compose.material.icons.outlined.Dns
import androidx.compose.material.icons.outlined.Favorite
import androidx.compose.material.icons.outlined.FactCheck
import androidx.compose.material.icons.outlined.Hub
import androidx.compose.material.icons.outlined.MonitorHeart
import androidx.compose.material.icons.outlined.NotificationsActive
import androidx.compose.material.icons.outlined.QueryStats
import androidx.compose.material.icons.outlined.Security
import androidx.compose.material.icons.outlined.Storage
import androidx.compose.material.icons.outlined.Sync
import androidx.compose.material.icons.outlined.SystemUpdateAlt
import androidx.compose.material.icons.outlined.Timeline
import androidx.compose.material.icons.outlined.Upgrade
import androidx.compose.material.icons.outlined.Widgets
import androidx.compose.ui.graphics.vector.ImageVector
import name.levis.ichor.R
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.Feature
import name.levis.ichor.model.NodeOverview
import name.levis.ichor.model.ScreenEntry
import name.levis.ichor.model.allows
import name.levis.ichor.model.isKube
import name.levis.ichor.ui.nav.Routes
import name.levis.ichor.ui.nav.nodeMenuRoute
import name.levis.ichor.ui.node.NodeMenuEntry
import name.levis.ichor.ui.node.icon
import name.levis.ichor.ui.settings.visibleSettingsPages

/** Where a screen sits, shown under its title: the cluster, its operations, Settings, or a node. */
enum class ScreenSection(@StringRes val title: Int) {
    CLUSTER(R.string.overview_title),
    OPERATE(R.string.node_menu_group_operate),
    SETTINGS(R.string.settings_title),
    NODE(0),
}

/**
 * A screen the search can open (U3 step 5). [title] is a string resource, or [literal] for the
 * names that are not translated (etcd, KubeSpan); [node] is the host of a node screen, shown
 * before its title. [route] is built when opened, so the catalog holds no encoded address.
 */
class ScreenSpec(
    val id: String,
    @StringRes val title: Int,
    val section: ScreenSection,
    val icon: ImageVector,
    val literal: String = "",
    @StringRes val subtitle: Int = 0,
    val node: String = "",
    val route: () -> String,
)

/**
 * Every screen of [cluster] the user may open, gated like the menus that lead to them: no
 * Talos screen for a cluster added from a kubeconfig, the Kubernetes ones only with a role
 * that reads the API, the upgrades only with one that may upgrade. [nodes] (the last overview)
 * adds each node's screens; none right after a cold start.
 */
fun screenCatalog(cluster: ContextSummary, nodes: List<NodeOverview> = emptyList()): List<ScreenSpec> {
    val talos = !cluster.isKube
    val kube = cluster.allows(Feature.WORKLOADS)
    val upgrade = cluster.allows(Feature.UPGRADE)
    val c = ScreenSection.CLUSTER
    val o = ScreenSection.OPERATE
    val clusterScreens = listOfNotNull(
        ScreenSpec("nodes", R.string.overview_stat_nodes, c, Icons.Outlined.Dns) { if (talos) Routes.nodes(null) else Routes.kubeNodes(null) },
        ScreenSpec("health", R.string.overview_action_health, c, Icons.Outlined.Favorite) { Routes.HEALTH }.takeIf { cluster.allows(Feature.HEALTH) },
        ScreenSpec("etcd", 0, c, Icons.Outlined.Storage, literal = "etcd") { Routes.ETCD }.takeIf { talos },
        ScreenSpec("events", R.string.overview_action_events, c, Icons.Outlined.Timeline) { Routes.events() }.takeIf { talos },
        ScreenSpec("kubespan", 0, c, Icons.Outlined.Hub, literal = "KubeSpan") { Routes.KUBESPAN }.takeIf { talos },
        ScreenSpec("workloads", R.string.overview_action_workloads, c, Icons.Outlined.Widgets) { Routes.workloads() }.takeIf { kube },
        ScreenSpec("alerts", R.string.alerts_title, c, Icons.Outlined.NotificationsActive) { Routes.alerts() }.takeIf { kube },
        ScreenSpec("metrics", R.string.metrics_title, c, Icons.Outlined.QueryStats) { Routes.metrics() }.takeIf { kube },
        ScreenSpec("argocd", R.string.argo_title, c, Icons.Outlined.Sync) { Routes.ARGO_CD }.takeIf { kube },
        ScreenSpec("flux", R.string.flux_title, c, Icons.Outlined.AccountTree) { Routes.FLUX }.takeIf { kube },
        ScreenSpec("data-services", R.string.data_services_title, c, Icons.Outlined.Storage) { Routes.dataServices() }.takeIf { kube },
        ScreenSpec("checkup", R.string.checkup_title, c, Icons.Outlined.FactCheck) { Routes.CHECKUP }.takeIf { kube },
        ScreenSpec("api-health", R.string.apihealth_title, c, Icons.Outlined.MonitorHeart) { Routes.API_HEALTH }.takeIf { kube },
        ScreenSpec("netpol", R.string.netpol_title, c, Icons.Outlined.Security) { Routes.NETWORK_POLICIES }.takeIf { kube },
        ScreenSpec("activity", R.string.activity_title, c, Icons.AutoMirrored.Outlined.ListAlt) { Routes.activity(cluster.name) },
    )
    // As the cluster title menu offers them (on Omni, Add a node opens the boot media).
    val operations = listOfNotNull(
        ScreenSpec("upgrade-cluster", R.string.cluster_upgrade_menu, o, Icons.Outlined.SystemUpdateAlt) { Routes.clusterUpgrade() }.takeIf { upgrade },
        ScreenSpec("upgrade-kubernetes", R.string.k8s_upgrade_menu, o, Icons.Outlined.Upgrade) { Routes.K8S_UPGRADE }.takeIf { upgrade },
        ScreenSpec("join-node", R.string.join_node_menu, o, Icons.Outlined.AddCircleOutline) { Routes.JOIN_NODE }.takeIf { talos },
        ScreenSpec("runbooks", R.string.runbooks_menu, o, Icons.Outlined.Checklist) { Routes.RUNBOOKS },
    )
    val settings = visibleSettingsPages(cluster).map { page ->
        ScreenSpec("settings/${page.route}", page.title, ScreenSection.SETTINGS, page.icon, subtitle = page.summary) { Routes.settingsPage(page) }
    }
    return clusterScreens + operations + settings + if (talos) nodeScreens(cluster, nodes) else emptyList()
}

/** Each node's sections with a screen of their own, as the node menu offers them to the role. */
private fun nodeScreens(cluster: ContextSummary, nodes: List<NodeOverview>): List<ScreenSpec> =
    nodes.distinctBy { it.node }.flatMap { node ->
        val host = node.hostname.ifEmpty { node.node }
        NodeMenuEntry.entries
            .filter { entry -> entry.tab == null && entry != NodeMenuEntry.CAPTURES && entry.role?.let(cluster::allows) != false }
            .filter { nodeMenuEntryHasScreen(it) }
            .map { entry ->
                ScreenSpec("node/${node.node}/${entry.name.lowercase()}", entry.label, ScreenSection.NODE, entry.icon, node = host) {
                    nodeMenuRoute(node.node, host, entry).orEmpty()
                }
            }
    }

/** The node menu entries [nodeMenuRoute] opens a screen for (the others are a confirmation). */
private fun nodeMenuEntryHasScreen(entry: NodeMenuEntry): Boolean =
    entry != NodeMenuEntry.RESET && entry != NodeMenuEntry.CORDON

/** [specs] as the Go core ranks them, every title resolved by [text] ("Settings → Alerts", "node-a → Hardware"). */
fun screenEntries(specs: List<ScreenSpec>, text: (Int) -> String): List<ScreenEntry> = specs.map { spec ->
    ScreenEntry(
        id = spec.id,
        title = screenTitle(spec, text),
        subtitle = if (spec.subtitle != 0) text(spec.subtitle) else "",
        section = if (spec.section == ScreenSection.NODE) spec.node else text(spec.section.title),
    )
}

/** The title a row shows: a menu label without its "…", after its node or Settings. */
fun screenTitle(spec: ScreenSpec, text: (Int) -> String): String {
    val title = spec.literal.ifEmpty { text(spec.title) }.removeSuffix("…")
    return when (spec.section) {
        ScreenSection.NODE -> "${spec.node} → $title"
        ScreenSection.SETTINGS -> "${text(R.string.settings_title)} → $title"
        else -> title
    }
}
