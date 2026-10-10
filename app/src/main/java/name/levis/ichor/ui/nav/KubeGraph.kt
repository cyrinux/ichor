package name.levis.ichor.ui.nav

import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.remember
import androidx.lifecycle.viewmodel.compose.viewModel
import androidx.navigation.NavGraphBuilder
import androidx.navigation.NavHostController
import androidx.navigation.NavType
import androidx.navigation.compose.composable
import androidx.navigation.navArgument
import name.levis.ichor.TalosApp
import name.levis.ichor.model.DataServiceKind
import name.levis.ichor.model.KubeFocus
import name.levis.ichor.model.NodeFilter
import name.levis.ichor.data.OVERVIEW
import name.levis.ichor.data.activeIsKube
import name.levis.ichor.model.ClusterOverview
import name.levis.ichor.model.KubeObjectRef
import name.levis.ichor.model.PromLink
import name.levis.ichor.model.ShareTarget
import name.levis.ichor.model.kubeFocus
import name.levis.ichor.ui.alerts.AlertObjectLinks
import name.levis.ichor.ui.alerts.AlertsScreen
import name.levis.ichor.ui.apihealth.ApiHealthScreen
import name.levis.ichor.ui.apihealth.AuditScreen
import name.levis.ichor.ui.apps.AppsScreen
import name.levis.ichor.ui.checkup.CheckupScreen
import name.levis.ichor.ui.debug.DebugShellScreen
import name.levis.ichor.ui.debug.ShellKey
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.flows.FlowsScreen
import name.levis.ichor.ui.kubebrowser.KubeBrowserRoutes
import name.levis.ichor.ui.kubebrowser.KubeLinks
import name.levis.ichor.ui.kubebrowser.LocalKubeLinks
import name.levis.ichor.ui.kubenodes.KubeNodesScreen
import name.levis.ichor.ui.metrics.METRICS_TAB_MONITORING
import name.levis.ichor.ui.netpol.NetworkPoliciesScreen
import name.levis.ichor.ui.overview.KubeHomeViewModel

/** The Kubernetes screens: nodes, workloads, apps, metrics, the checkup, flows, data services, pod shells. */
internal fun NavGraphBuilder.kubeGraph(nav: NavHostController, app: TalosApp, kubeLinks: KubeLinks) {
    with(KubeBrowserRoutes) { kubeBrowserScreens(nav, kubeLinks) }
    composable(Routes.METRICS, arguments = listOf(navArgument("tab") { type = NavType.IntType; defaultValue = 0 })) { entry ->
        name.levis.ichor.ui.metrics.MetricsScreen(
            onBack = { nav.popBackStack() },
            onSettings = { nav.navigate(Routes.SETTINGS) },
            onLink = { link ->
                when (link) {
                    is PromLink.Focus -> nav.navigate(Routes.workloads(link.focus))
                    is PromLink.Object -> kubeLinks.onObject(link.ref)
                }
            },
            initialTab = entry.arguments?.getInt("tab") ?: 0,
        )
    }
    composable(Routes.ALERTS, arguments = listOf(navArgument("silence") { type = NavType.StringType; defaultValue = "" })) { entry ->
        AlertsScreen(
            onBack = { nav.popBackStack() },
            links = remember(nav) { alertLinks(nav, app) },
            silenceFingerprint = entry.arguments?.getString("silence").orEmpty(),
        )
    }
    composable(Routes.KUBE_NODES, arguments = listOf(navArgument("filter") { type = NavType.StringType; defaultValue = "" })) { entry ->
        // The Kubernetes home's data and refresh: only ever opened from it, so it is below on the stack.
        val home = remember(entry) { nav.getBackStackEntry(Routes.OVERVIEW) }
        KubeNodesScreen(
            initialFilter = NodeFilter.entries.firstOrNull { it.name == entry.arguments?.getString("filter") },
            vm = viewModel(viewModelStoreOwner = home, factory = factory { KubeHomeViewModel(app.kubeRepository) }),
            onBack = { nav.popBackStack() },
            onDrain = { nav.navigate(Routes.maintenance(it, it, drain = true)) },
            onDebug = { nav.navigate(Routes.kubeNodeDebug(app.configRepository.config.value?.activeContext.orEmpty(), it)) },
        )
    }
    composable(
        Routes.KUBE_NODE_DEBUG,
        arguments = listOf("ctx", "node", "ns").map { navArgument(it) { type = NavType.StringType; defaultValue = "" } },
    ) { entry ->
        val a = entry.arguments
        val node = a?.getString("node").orEmpty()
        DebugShellScreen(
            key = ShellKey(a?.getString("ctx").orEmpty(), node, a?.getString("ns").orEmpty().ifEmpty { "default" }, kubeNode = true),
            hostname = node,
            onBack = { nav.popBackStack() },
        )
    }
    composable(
        Routes.POD_SHELL,
        arguments = listOf("ctx", "ns", "pod", "c").map { navArgument(it) { type = NavType.StringType; defaultValue = "" } },
    ) { entry ->
        val a = entry.arguments
        val pod = a?.getString("pod").orEmpty()
        DebugShellScreen(
            key = ShellKey(a?.getString("ctx").orEmpty(), "", a?.getString("ns").orEmpty(), pod, a?.getString("c").orEmpty()),
            hostname = pod,
            onBack = { nav.popBackStack() },
        )
    }
    composable(
        Routes.APPS,
        arguments = listOf(navArgument("attention") { type = NavType.BoolType; defaultValue = false }),
    ) { entry ->
        AppsScreen(
            attention = entry.arguments?.getBoolean("attention") == true,
            onBack = { nav.popBackStack() },
            // A pod's node, on its Pods tab.
            onNode = { addr, host, role -> nav.navigate(Routes.node(addr, host, role, tab = 4)) },
            onArgoCD = { nav.navigate(Routes.ARGO_CD) },
            onArgoApp = { ns, name -> nav.navigate(Routes.argoApp(ns, name)) },
            onFlux = { nav.navigate(Routes.FLUX) },
        )
    }
    composable(
        Routes.WORKLOADS,
        arguments = listOf(
            navArgument("tab") { type = NavType.IntType; defaultValue = 0 },
            navArgument("key") { type = NavType.StringType; defaultValue = "" },
            navArgument("ns") { type = NavType.StringType; defaultValue = "" },
            navArgument("name") { type = NavType.StringType; defaultValue = "" },
        ),
    ) { entry ->
        val args = entry.arguments
        CompositionLocalProvider(LocalKubeLinks provides kubeLinks) {
            name.levis.ichor.ui.workloads.KubernetesScreen(
                focus = KubeFocus(
                    args?.getInt("tab") ?: 0,
                    args?.getString("key").orEmpty(),
                    args?.getString("ns").orEmpty(),
                    args?.getString("name").orEmpty(),
                ),
                onBack = { nav.popBackStack() },
                onNetworkPolicies = { nav.navigate(Routes.NETWORK_POLICIES) },
                onApiHealth = { nav.navigate(Routes.API_HEALTH) },
                onCheckup = { nav.navigate(Routes.CHECKUP) },
                onFlows = { ns, pod -> nav.navigate(Routes.flows(ns, pod)) },
                onResources = { nav.navigate(KubeBrowserRoutes.KINDS) },
                onHelm = { nav.navigate(KubeBrowserRoutes.HELM) },
                onStorage = { nav.navigate(KubeBrowserRoutes.STORAGE) },
                onServices = { nav.navigate(KubeBrowserRoutes.SERVICES) },
                onJobs = { nav.navigate(KubeBrowserRoutes.JOBS) },
            )
        }
    }
    composable(Routes.NETWORK_POLICIES) { NetworkPoliciesScreen(onBack = { nav.popBackStack() }) }
    composable(Routes.API_HEALTH) { ApiHealthScreen(onBack = { nav.popBackStack() }, onAudit = { nav.navigate(Routes.AUDIT) }) }
    composable(Routes.AUDIT) { AuditScreen(onBack = { nav.popBackStack() }) }
    composable(Routes.CHECKUP) {
        CheckupScreen(
            onBack = { nav.popBackStack() },
            onOpenRelease = { ns, name -> nav.navigate(KubeBrowserRoutes.helmRelease(ns, name)) },
            onOpenMonitoring = { nav.navigate(Routes.metrics(METRICS_TAB_MONITORING)) },
        )
    }
    composable(
        Routes.FLOWS,
        arguments = listOf(
            navArgument("ns") { type = NavType.StringType; defaultValue = "" },
            navArgument("pod") { type = NavType.StringType; defaultValue = "" },
        ),
    ) { entry ->
        FlowsScreen(
            onBack = { nav.popBackStack() },
            namespace = entry.arguments?.getString("ns")?.ifEmpty { null },
            pod = entry.arguments?.getString("pod")?.ifEmpty { null },
        )
    }
    composable(
        Routes.DATA_SERVICES,
        arguments = listOf(navArgument("kind") { type = NavType.StringType; defaultValue = "" }),
    ) { entry ->
        val kind = entry.arguments?.getString("kind")?.let { k -> DataServiceKind.entries.firstOrNull { it.name == k } }
        name.levis.ichor.ui.dataservices.DataServicesScreen(
            initial = kind,
            onBack = { nav.popBackStack() },
            onSupportedIntegrations = { nav.navigate(Routes.SUPPORTED_INTEGRATIONS) },
            onRequestIntegration = { nav.navigate(Routes.INTEGRATIONS) },
        )
    }
}

/**
 * Where an alert's labels lead: a pod or a namespace on the Kubernetes screen (focused as a
 * share link focuses it), a node on its screen (Talos), or its object (a cluster added from a
 * kubeconfig, which has no node screen).
 */
private fun alertLinks(nav: NavHostController, app: TalosApp) = AlertObjectLinks(
    onPod = { ns, pod -> ShareTarget.pod(ns, pod).kubeFocus?.let { nav.navigate(Routes.workloads(it)) } },
    onNamespace = { ns -> nav.navigate(Routes.workloads(KubeFocus(0, namespace = ns))) },
    onNode = { name ->
        if (app.configRepository.config.value?.activeIsKube == true) {
            nav.navigate(KubeBrowserRoutes.obj(KubeObjectRef("", "v1", "nodes", "Node", "", name, editable = false)))
        } else {
            app.talosRepository.cached<ClusterOverview>(OVERVIEW)?.value?.nodes
                ?.firstOrNull { it.hostname == name || it.node == name }
                ?.let { nav.navigate(Routes.node(it.node, it.hostname, it.role)) }
        }
    },
)
