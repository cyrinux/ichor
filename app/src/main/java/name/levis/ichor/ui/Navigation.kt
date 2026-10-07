package name.levis.ichor.ui

import android.net.Uri
import android.widget.Toast
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.LocalContext
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import androidx.navigation.NavBackStackEntry
import androidx.navigation.NavHostController
import androidx.navigation.NavType
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.rememberNavController
import androidx.navigation.navArgument
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.activeSummary
import name.levis.ichor.data.activeIsKube
import name.levis.ichor.model.DataServiceKind
import name.levis.ichor.model.Feature
import name.levis.ichor.model.KubeFocus
import name.levis.ichor.model.LogSource
import name.levis.ichor.model.NodeFilter
import name.levis.ichor.model.NodeOverview
import name.levis.ichor.model.ShareTarget
import name.levis.ichor.model.allows
import name.levis.ichor.model.containerLogSubtitle
import name.levis.ichor.model.containerLogTitle
import name.levis.ichor.model.contextFor
import name.levis.ichor.model.kubeFocus
import name.levis.ichor.model.nodeTab
import name.levis.ichor.model.sensitive
import name.levis.ichor.ui.apihealth.ApiHealthScreen
import name.levis.ichor.ui.apihealth.AuditScreen
import name.levis.ichor.ui.apps.AppsScreen
import name.levis.ichor.ui.argocd.ArgoAppScreen
import name.levis.ichor.ui.argocd.ArgoAppsScreen
import name.levis.ichor.ui.argocd.ArgoWindowsScreen
import name.levis.ichor.ui.backup.IncomingBackup
import name.levis.ichor.ui.capture.CaptureFileScreen
import name.levis.ichor.ui.checkup.CheckupScreen
import name.levis.ichor.ui.capture.CaptureScreen
import name.levis.ichor.ui.capture.CapturesScreen
import name.levis.ichor.ui.changelog.ChangelogScreen
import name.levis.ichor.ui.changelog.WhatsNewHost
import name.levis.ichor.ui.debug.DebugShellScreen
import name.levis.ichor.ui.debug.LiveShell
import name.levis.ichor.ui.diagnosis.DiagnosisScreen
import name.levis.ichor.ui.etcd.EtcdScreen
import name.levis.ichor.ui.events.EventsScreen
import name.levis.ichor.ui.flows.FlowsScreen
import name.levis.ichor.ui.flux.FluxAppScreen
import name.levis.ichor.ui.flux.FluxDiffScreen
import name.levis.ichor.ui.flux.FluxScreen
import name.levis.ichor.ui.funding.FundingScreen
import name.levis.ichor.ui.hardware.HardwareScreen
import name.levis.ichor.ui.health.HealthScreen
import name.levis.ichor.ui.images.ImagesScreen
import name.levis.ichor.ui.importconfig.ImportScreen
import name.levis.ichor.ui.integrations.IntegrationsScreen
import name.levis.ichor.ui.issueconfig.IssueConfigScreen
import name.levis.ichor.ui.kubebrowser.KubeBrowserRoutes
import name.levis.ichor.ui.kubebrowser.KubeLinks
import name.levis.ichor.ui.kubebrowser.LocalKubeLinks
import name.levis.ichor.ui.kubespan.KubeSpanScreen
import name.levis.ichor.ui.logs.LogsScreen
import name.levis.ichor.ui.machineconfig.MachineConfigScreen
import name.levis.ichor.ui.maintenance.MaintenanceScreen
import name.levis.ichor.ui.netpol.NetworkPoliciesScreen
import name.levis.ichor.ui.network.NetworkScreen
import name.levis.ichor.ui.node.NodeDetailScreen
import name.levis.ichor.ui.node.NodeMenuEntry
import name.levis.ichor.ui.node.PowerAction
import name.levis.ichor.ui.nodes.NodesScreen
import name.levis.ichor.ui.overview.NodeAction
import name.levis.ichor.ui.overview.OverviewScreen
import name.levis.ichor.ui.overview.OverviewViewModel
import name.levis.ichor.ui.resources.ResourceDetailScreen
import name.levis.ichor.ui.resources.ResourceListScreen
import name.levis.ichor.ui.resources.ResourceRef
import name.levis.ichor.ui.resources.ResourceTypesScreen
import name.levis.ichor.ui.settings.LicensesScreen
import name.levis.ichor.ui.settings.SettingsScreen
import name.levis.ichor.ui.settings.SupportedIntegrationsScreen
import name.levis.ichor.ui.share.parseShareLink
import name.levis.ichor.ui.storage.StorageScreen
import name.levis.ichor.ui.support.SupportBundleScreen
import name.levis.ichor.ui.upgrade.UpgradeScreen

private object Routes {
    const val IMPORT = "import"
    const val DEMO = "demo"
    const val OVERVIEW = "overview"
    const val NODE = "node?addr={addr}&host={host}&role={role}&tab={tab}&action={action}"
    const val LOGS = "logs?addr={addr}&host={host}&service={service}&container={container}&title={title}&subtitle={subtitle}"
    const val STORAGE = "storage?addr={addr}&host={host}"
    const val RESOURCES = "resources?addr={addr}&host={host}"
    const val RESOURCE_LIST = "resourcelist?addr={addr}&host={host}&ns={ns}&type={type}&sensitive={sensitive}"
    const val RESOURCE = "resource?addr={addr}&host={host}&ns={ns}&type={type}&id={id}&sensitive={sensitive}"
    const val SUPPORT_BUNDLE = "supportbundle"
    const val INTEGRATIONS = "integrations"
    const val CHANGELOG = "changelog"
    const val LICENSES = "licenses"
    const val SUPPORTED_INTEGRATIONS = "supported-integrations"
    const val FUNDING = "funding"
    const val INSIGHTS = "insights"
    const val APPS = "apps?attention={attention}"
    const val METRICS = "metrics"

    /** [attention]: open on the "needs attention" chip. */
    fun apps(attention: Boolean = false) = "apps?attention=$attention"

    fun storage(addr: String, host: String) = "storage?addr=${Uri.encode(addr)}&host=${Uri.encode(host)}"

    fun resources(addr: String, host: String) = "resources?addr=${Uri.encode(addr)}&host=${Uri.encode(host)}"

    fun resourceList(addr: String, host: String, namespace: String, type: String, sensitive: Boolean) =
        "resourcelist?addr=${Uri.encode(addr)}&host=${Uri.encode(host)}&ns=${Uri.encode(namespace)}&type=${Uri.encode(type)}&sensitive=$sensitive"

    fun resource(addr: String, host: String, ref: ResourceRef, id: String) =
        "resource?addr=${Uri.encode(addr)}&host=${Uri.encode(host)}&ns=${Uri.encode(ref.namespace)}&type=${Uri.encode(ref.type)}" +
            "&id=${Uri.encode(id)}&sensitive=${ref.sensitive}"

    /** The log of a CRI container: [title] its name, [subtitle] its "namespace/pod". */
    fun containerLogs(addr: String, host: String, container: String, title: String, subtitle: String) =
        "logs?addr=${Uri.encode(addr)}&host=${Uri.encode(host)}&container=${Uri.encode(container)}" +
            "&title=${Uri.encode(title)}&subtitle=${Uri.encode(subtitle)}"
    const val ETCD = "etcd"
    const val KUBESPAN = "kubespan"
    const val NODES = "nodes?filter={filter}"

    /** The Nodes screen of a large cluster; [filter] preselects one (null: all). */
    fun nodes(filter: NodeFilter?) = "nodes?filter=${filter?.name.orEmpty()}"
    const val WORKLOADS = "workloads?tab={tab}&key={key}&ns={ns}&name={name}"

    /** The Kubernetes screen; [focus] opens a tab and shows one of its items (a share link). */
    fun workloads(focus: KubeFocus? = null) = if (focus == null) "workloads" else
        "workloads?tab=${focus.tab}&key=${Uri.encode(focus.key)}&ns=${Uri.encode(focus.namespace)}&name=${Uri.encode(focus.name)}"
    const val NETWORK_POLICIES = "netpol"
    const val API_HEALTH = "apihealth"
    const val CHECKUP = "checkup"
    const val AUDIT = "audit"
    const val FLOWS = "flows?ns={ns}&pod={pod}"

    /** Empty [namespace] for all of them; [pod] narrows to one of [namespace]. */
    fun flows(namespace: String?, pod: String?) = "flows?ns=${Uri.encode(namespace.orEmpty())}&pod=${Uri.encode(pod.orEmpty())}"
    const val DATA_SERVICES = "data-services?kind={kind}"

    /** [kind] opens on that system's tab; null on the first. */
    fun dataServices(kind: DataServiceKind? = null) = "data-services?kind=${kind?.name.orEmpty()}"
    const val ARGO_CD = "argocd"
    const val ARGO_APP = "argocd-app?ns={ns}&name={name}"
    const val ARGO_WINDOWS = "argocd-windows"

    fun argoApp(namespace: String, name: String) = "argocd-app?ns=${Uri.encode(namespace)}&name=${Uri.encode(name)}"
    const val FLUX = "flux"
    const val FLUX_APP = "flux-app?kind={kind}&ns={ns}&name={name}"

    fun fluxApp(kind: String, namespace: String, name: String) =
        "flux-app?kind=${Uri.encode(kind)}&ns=${Uri.encode(namespace)}&name=${Uri.encode(name)}"
    const val FLUX_DIFF = "flux-diff?kind={kind}&ns={ns}&name={name}"
    fun fluxDiff(kind: String, namespace: String, name: String) =
        "flux-diff?kind=${Uri.encode(kind)}&ns=${Uri.encode(namespace)}&name=${Uri.encode(name)}"
    const val DEBUG = "debug?addr={addr}&host={host}&ctx={ctx}"
    const val MACHINE_CONFIG = "machineconfig?addr={addr}&host={host}"
    const val NETWORK = "network?addr={addr}&host={host}"
    const val HARDWARE = "hardware?addr={addr}&host={host}"
    const val IMAGES = "images?addr={addr}&host={host}"
    const val ISSUE_CONFIG = "issueconfig"
    const val CAPTURE = "capture?addr={addr}&host={host}"
    const val CAPTURES = "captures"
    const val CAPTURE_FILE = "capturefile?name={name}"
    const val UPGRADE = "upgrade?addr={addr}&host={host}&version={version}"
    const val MAINTENANCE = "maintenance?addr={addr}&host={host}"

    fun maintenance(addr: String, host: String) = "maintenance?addr=${Uri.encode(addr)}&host=${Uri.encode(host)}"

    fun capture(addr: String, host: String) = "capture?addr=${Uri.encode(addr)}&host=${Uri.encode(host)}"

    fun captureFile(name: String) = "capturefile?name=${Uri.encode(name)}"

    /** [version]: target version to preselect (empty: none). */
    fun upgrade(addr: String, host: String, version: String = "") =
        "upgrade?addr=${Uri.encode(addr)}&host=${Uri.encode(host)}&version=${Uri.encode(version)}"

    fun network(addr: String, host: String) = "network?addr=${Uri.encode(addr)}&host=${Uri.encode(host)}"

    fun hardware(addr: String, host: String) = "hardware?addr=${Uri.encode(addr)}&host=${Uri.encode(host)}"

    fun images(addr: String, host: String) = "images?addr=${Uri.encode(addr)}&host=${Uri.encode(host)}"

    /** Empty addr: events of every node of the context. */
    const val EVENTS = "events?addr={addr}&host={host}"

    fun events(addr: String = "", host: String = "") = "events?addr=${Uri.encode(addr)}&host=${Uri.encode(host)}"

    fun machineConfig(addr: String, host: String) = "machineconfig?addr=${Uri.encode(addr)}&host=${Uri.encode(host)}"

    /** [context]: the cluster (talosconfig context) of the node; blank for the one on screen. */
    fun debug(addr: String, host: String, context: String = "") =
        "debug?addr=${Uri.encode(addr)}&host=${Uri.encode(host)}&ctx=${Uri.encode(context)}"
    const val HEALTH = "health"
    const val SETTINGS = "settings"
    const val DIAGNOSIS = "diagnosis?note={note}"

    /** [note]: what to tell the model up front, e.g. a failed health check. */
    fun diagnosis(note: String = "") = "diagnosis?note=${Uri.encode(note)}"

    /** Empty [service] means the kernel log. */
    fun logs(addr: String, host: String, service: String?) =
        "logs?addr=${Uri.encode(addr)}&host=${Uri.encode(host)}&service=${Uri.encode(service.orEmpty())}"

    /** [tab]: 0 services, 1 resources, 2 live, 3 processes, 4 pods; [action]: "reboot"/"shutdown" opens its confirmation. */
    fun node(addr: String, host: String, role: String, tab: Int = 0, action: String = "") =
        "node?addr=${Uri.encode(addr)}&host=${Uri.encode(host)}&role=${Uri.encode(role)}&tab=$tab&action=$action"
}

/** Screens a notification can open directly (see MainActivity.EXTRA_OPEN). */
enum class DeepLink { ISSUE_CONFIG, DEMO, ARGO_WINDOWS, ARGO_CD, FLUX, CHECKUP }

/**
 * [deepLink]: a screen to open once over the overview; [onDeepLinkHandled] then clears it.
 * [openCluster]: the fingerprint of a cluster to show (a launcher shortcut), cleared by [onClusterOpened].
 * [incomingBackup]: a backup file opened from another app, to restore; cleared by [onIncomingBackupRead].
 * [openShell]: a debug shell to go back to (its notification), cleared by [onShellOpened].
 * [openLink]: a share link, to open on the cluster it names once unlocked; cleared by [onLinkOpened].
 */
@Composable
fun Navigation(
    app: TalosApp,
    startWithImport: Boolean,
    deepLink: DeepLink? = null,
    onDeepLinkHandled: () -> Unit = {},
    openCluster: String? = null,
    onClusterOpened: () -> Unit = {},
    incomingBackup: Uri? = null,
    onIncomingBackupRead: () -> Unit = {},
    openShell: LiveShell? = null,
    onShellOpened: () -> Unit = {},
    openLink: String? = null,
    onLinkOpened: () -> Unit = {},
) {
    val nav = rememberNavController()
    val context = LocalContext.current
    val linkLocked by app.appLock.locked.collectAsStateWithLifecycle()
    // A config opened with the app, until the import screen took it. Not saveable: it holds
    // credentials, which must not land in saved instance state.
    var incomingConfig by remember { mutableStateOf<String?>(null) }

    // Like a launcher shortcut, on the overview of the cluster the link names (the context on
    // screen when it is one of that cluster), then the screen it names over it. Read once
    // unlocked, with the config loaded: screenshot mode then masks its names like the screens'.
    LaunchedEffect(openLink, linkLocked) {
        val link = openLink ?: return@LaunchedEffect
        if (linkLocked) return@LaunchedEffect
        val target = parseShareLink(link)
        val stored = app.configRepository.config.value
        val cluster = target?.let { stored?.summary?.contextFor(it.cluster, stored.activeContext) }
        when {
            target == null -> Toast.makeText(context, R.string.share_link_invalid, Toast.LENGTH_LONG).show()
            cluster == null -> Toast.makeText(context, R.string.share_link_unknown_cluster, Toast.LENGTH_LONG).show()
            else -> {
                app.selectCluster(cluster.name)
                nav.resetTo(Routes.OVERVIEW)
                target.route(app)?.let { nav.navigate(it) }
            }
        }
        // Last: clearing the link restarts this effect, which would cancel a node lookup.
        onLinkOpened()
    }

    // Over whatever is on screen, unless it already is that shell.
    LaunchedEffect(openShell) {
        val shell = openShell ?: return@LaunchedEffect
        val top = nav.currentBackStackEntry
        val active = app.configRepository.config.value?.activeContext.orEmpty()
        val showing = top?.destination?.route == Routes.DEBUG &&
            top.arguments?.getString("addr") == shell.key.node &&
            top.arguments?.getString("ctx").orEmpty().ifEmpty { active } == shell.key.context
        if (!startWithImport && !showing) nav.navigate(Routes.debug(shell.key.node, shell.hostname, shell.key.context))
        onShellOpened()
    }

    // Back on that cluster's overview: a screen of the previous one must not stay open over it.
    LaunchedEffect(openCluster) {
        if (openCluster == null) return@LaunchedEffect
        app.configRepository.config.value?.summary?.contexts
            ?.firstOrNull { it.fingerprint == openCluster }
            ?.let { cluster ->
                app.selectCluster(cluster.name)
                nav.resetTo(Routes.OVERVIEW)
            }
        onClusterOpened()
    }

    // The Argo CD and Flux screens need a role allowed the Kubernetes API.
    fun canUseKube() = app.configRepository.config.value?.activeSummary?.allows(Feature.WORKLOADS) == true

    LaunchedEffect(deepLink) {
        if (deepLink == null) return@LaunchedEffect
        when (deepLink) {
            DeepLink.DEMO -> nav.navigate(Routes.DEMO) { launchSingleTop = true }
            // The certificate alert of a Talos cluster: the one on screen may be a kubeconfig one since.
            DeepLink.ISSUE_CONFIG -> if (!startWithImport && app.configRepository.config.value?.activeSummary?.allows(Feature.ISSUE_CONFIG) == true) {
                nav.navigate(Routes.ISSUE_CONFIG) { launchSingleTop = true }
            }
            DeepLink.ARGO_WINDOWS -> if (!startWithImport) {
                nav.navigate(Routes.ARGO_WINDOWS) { launchSingleTop = true }
            }
            // GitOps alerts: only for a role that may use the Kubernetes API (the active cluster may
            // have changed since the alert was posted).
            DeepLink.ARGO_CD -> if (!startWithImport && canUseKube()) {
                nav.navigate(Routes.ARGO_CD) { launchSingleTop = true }
            }
            DeepLink.FLUX -> if (!startWithImport && canUseKube()) {
                nav.navigate(Routes.FLUX) { launchSingleTop = true }
            }
            DeepLink.CHECKUP -> if (!startWithImport && canUseKube()) {
                nav.navigate(Routes.CHECKUP) { launchSingleTop = true }
            }
        }
        onDeepLinkHandled()
    }

    // Browser screens a pod's sheet opens: its YAML, a port-forward.
    val kubeLinks = remember(nav) {
        KubeLinks(
            onObject = { nav.navigate(KubeBrowserRoutes.obj(it)) },
            onPortForward = { ns, pod -> nav.navigate(KubeBrowserRoutes.forward(ns, pod)) },
        )
    }

    NavHost(navController = nav, startDestination = if (startWithImport) Routes.IMPORT else Routes.OVERVIEW) {
        with(KubeBrowserRoutes) { kubeBrowserScreens(nav, kubeLinks) }
        composable(Routes.INSIGHTS) { name.levis.ichor.ui.insights.InsightsScreen(onBack = { nav.popBackStack() }) }
        composable(Routes.METRICS) { name.levis.ichor.ui.metrics.MetricsScreen(onBack = { nav.popBackStack() }) }
        composable(Routes.IMPORT) {
            ImportScreen(
                incoming = incomingConfig,
                onIncomingTaken = { incomingConfig = null },
                onImported = {
                    app.launchSync(runNow = true)
                    nav.resetTo(Routes.OVERVIEW)
                },
                // Adding a cluster comes from the overview or the settings: one can go back.
                onBack = if (nav.previousBackStackEntry != null) ({ nav.popBackStack() }) else null,
            )
        }
        composable(Routes.DEMO) {
            ImportScreen(
                autoStartDemo = true,
                onImported = {
                    app.launchSync(runNow = true)
                    nav.resetTo(Routes.OVERVIEW)
                },
                onBack = { nav.popBackStack() },
            )
        }
        composable(Routes.OVERVIEW) {
            OverviewScreen(
                onNode = { nav.navigate(Routes.node(it.node, it.hostname, it.role)) },
                onNodeAction = nav::openNodeAction,
                onEtcd = { nav.navigate(Routes.ETCD) },
                onKubeSpan = { nav.navigate(Routes.KUBESPAN) },
                onWorkloads = { nav.navigate(Routes.workloads()) },
                onMetrics = { nav.navigate(Routes.METRICS) },
                onDataServices = { nav.navigate(Routes.dataServices(it)) },
                onArgoCD = { nav.navigate(Routes.ARGO_CD) },
                onFlux = { nav.navigate(Routes.FLUX) },
                onHealth = { nav.navigate(Routes.HEALTH) },
                onEvents = { nav.navigate(Routes.events()) },
                onInsights = { nav.navigate(Routes.INSIGHTS) },
                onApps = { attention -> nav.navigate(Routes.apps(attention)) },
                onSettings = { nav.navigate(Routes.SETTINGS) },
                onFunding = { nav.navigate(Routes.FUNDING) },
                onIssueConfig = { nav.navigate(Routes.ISSUE_CONFIG) },
                onUpgrade = { n, version -> nav.navigate(Routes.upgrade(n.node, n.hostname, version)) },
                onDiagnose = { nav.navigate(Routes.diagnosis()) },
                onAddCluster = { nav.navigate(Routes.IMPORT) },
                onClustersCleared = { nav.resetTo(Routes.IMPORT) },
                onChangelog = { nav.navigate(Routes.CHANGELOG) },
                onAllNodes = { nav.navigate(Routes.nodes(it)) },
                onCheckup = { nav.navigate(Routes.CHECKUP) },
                onApiHealth = { nav.navigate(Routes.API_HEALTH) },
                onNetworkPolicies = { nav.navigate(Routes.NETWORK_POLICIES) },
                onResources = { nav.navigate(KubeBrowserRoutes.KINDS) },
                onHelm = { nav.navigate(KubeBrowserRoutes.HELM) },
            )
            // After an update: what changed since the build that ran before.
            WhatsNewHost(onFullChangelog = { nav.navigate(Routes.CHANGELOG) })
        }
        composable(Routes.NODES, arguments = listOf(navArgument("filter") { type = NavType.StringType; defaultValue = "" })) { entry ->
            // The overview's data and refresh: only ever opened from it, so it is below on the stack.
            val home = remember(entry) { nav.getBackStackEntry(Routes.OVERVIEW) }
            NodesScreen(
                initialFilter = NodeFilter.entries.firstOrNull { it.name == entry.arguments?.getString("filter") },
                vm = viewModel(viewModelStoreOwner = home, factory = factory { OverviewViewModel(app.talosRepository, app.configRepository) }),
                onBack = { nav.popBackStack() },
                onNode = { nav.navigate(Routes.node(it.node, it.hostname, it.role)) },
                onNodeAction = nav::openNodeAction,
            )
        }
        composable(
            Routes.NODE,
            arguments = listOf(
                navArgument("addr") { type = NavType.StringType },
                navArgument("host") { type = NavType.StringType },
                navArgument("role") { type = NavType.StringType; defaultValue = "unknown" },
                navArgument("tab") { type = NavType.IntType; defaultValue = 0 },
                navArgument("action") { type = NavType.StringType; defaultValue = "" },
            ),
        ) { entry ->
            val addr = entry.arguments?.getString("addr").orEmpty()
            val host = entry.arguments?.getString("host") ?: addr
            CompositionLocalProvider(LocalKubeLinks provides kubeLinks) {
                NodeDetailScreen(
                    node = addr,
                    hostname = host,
                    role = entry.arguments?.getString("role") ?: "unknown",
                    initialTab = entry.arguments?.getInt("tab") ?: 0,
                    initialAction = when (entry.arguments?.getString("action")) {
                        "reboot" -> PowerAction.REBOOT
                        "shutdown" -> PowerAction.SHUTDOWN
                        else -> null
                    },
                    onBack = { nav.popBackStack() },
                    onLogs = { service -> nav.navigate(Routes.logs(addr, host, service)) },
                    onContainerLogs = { c ->
                        nav.navigate(Routes.containerLogs(addr, host, c.id, containerLogTitle(c), containerLogSubtitle(c)))
                    },
                    onMenu = { item ->
                        when (item) {
                            NodeMenuEntry.KERNEL_LOG -> Routes.logs(addr, host, null)
                            NodeMenuEntry.EVENTS -> Routes.events(addr, host)
                            NodeMenuEntry.NETWORK -> Routes.network(addr, host)
                            NodeMenuEntry.HARDWARE -> Routes.hardware(addr, host)
                            NodeMenuEntry.IMAGES -> Routes.images(addr, host)
                            NodeMenuEntry.STORAGE -> Routes.storage(addr, host)
                            NodeMenuEntry.RESOURCES -> Routes.resources(addr, host)
                            NodeMenuEntry.DEBUG_SHELL -> Routes.debug(addr, host)
                            NodeMenuEntry.CAPTURE -> Routes.capture(addr, host)
                            NodeMenuEntry.CAPTURES -> Routes.CAPTURES
                            NodeMenuEntry.MACHINE_CONFIG -> Routes.machineConfig(addr, host)
                            NodeMenuEntry.UPGRADE -> Routes.upgrade(addr, host)
                            NodeMenuEntry.MAINTENANCE -> Routes.maintenance(addr, host)
                            // Handled on the node screen (a confirmation, no screen of its own).
                            NodeMenuEntry.CORDON -> null
                        }?.let { nav.navigate(it) }
                    },
                )
            }
        }
        composable(
            Routes.MACHINE_CONFIG,
            arguments = listOf(
                navArgument("addr") { type = NavType.StringType },
                navArgument("host") { type = NavType.StringType },
            ),
        ) { entry ->
            val addr = entry.arguments?.getString("addr").orEmpty()
            MachineConfigScreen(node = addr, hostname = entry.arguments?.getString("host") ?: addr, onBack = { nav.popBackStack() })
        }
        composable(Routes.NETWORK, arguments = nodeArguments()) { entry ->
            val addr = entry.arguments?.getString("addr").orEmpty()
            NetworkScreen(node = addr, hostname = entry.arguments?.getString("host") ?: addr, onBack = { nav.popBackStack() })
        }
        composable(Routes.HARDWARE, arguments = nodeArguments()) { entry ->
            val addr = entry.arguments?.getString("addr").orEmpty()
            HardwareScreen(node = addr, hostname = entry.arguments?.getString("host") ?: addr, onBack = { nav.popBackStack() })
        }
        composable(Routes.IMAGES, arguments = nodeArguments()) { entry ->
            val addr = entry.arguments?.getString("addr").orEmpty()
            ImagesScreen(node = addr, hostname = entry.arguments?.getString("host") ?: addr, onBack = { nav.popBackStack() })
        }
        composable(Routes.CAPTURE, arguments = nodeArguments()) { entry ->
            val addr = entry.arguments?.getString("addr").orEmpty()
            CaptureScreen(
                node = addr,
                hostname = entry.arguments?.getString("host") ?: addr,
                onBack = { nav.popBackStack() },
                onCaptures = { nav.navigate(Routes.CAPTURES) },
            )
        }
        composable(Routes.CAPTURES) {
            CapturesScreen(onBack = { nav.popBackStack() }, onOpen = { nav.navigate(Routes.captureFile(it)) })
        }
        composable(Routes.CAPTURE_FILE, arguments = listOf(navArgument("name") { type = NavType.StringType })) { entry ->
            CaptureFileScreen(name = entry.arguments?.getString("name").orEmpty(), onBack = { nav.popBackStack() })
        }
        composable(
            Routes.UPGRADE,
            arguments = nodeArguments() + navArgument("version") { type = NavType.StringType; defaultValue = "" },
        ) { entry ->
            val addr = entry.arguments?.getString("addr").orEmpty()
            UpgradeScreen(
                node = addr,
                hostname = entry.arguments?.getString("host") ?: addr,
                initialVersion = entry.arguments?.getString("version").orEmpty(),
                onBack = { nav.popBackStack() },
            )
        }
        composable(Routes.MAINTENANCE, arguments = nodeArguments()) { entry ->
            val addr = entry.arguments?.getString("addr").orEmpty()
            MaintenanceScreen(node = addr, hostname = entry.arguments?.getString("host") ?: addr, onBack = { nav.popBackStack() })
        }
        composable(Routes.ISSUE_CONFIG) { IssueConfigScreen(onBack = { nav.popBackStack() }) }
        composable(
            Routes.LOGS,
            arguments = listOf(
                navArgument("addr") { type = NavType.StringType },
                navArgument("host") { type = NavType.StringType },
                navArgument("service") { type = NavType.StringType; defaultValue = "" },
                navArgument("container") { type = NavType.StringType; defaultValue = "" },
                navArgument("title") { type = NavType.StringType; defaultValue = "" },
                navArgument("subtitle") { type = NavType.StringType; defaultValue = "" },
            ),
        ) { entry ->
            val addr = entry.arguments?.getString("addr").orEmpty()
            val container = entry.arguments?.getString("container").orEmpty()
            LogsScreen(
                node = addr,
                hostname = entry.arguments?.getString("host") ?: addr,
                source = if (container.isEmpty()) {
                    LogSource.Service(entry.arguments?.getString("service")?.takeIf { it.isNotEmpty() })
                } else {
                    LogSource.Container(
                        id = container,
                        title = entry.arguments?.getString("title").orEmpty().ifEmpty { container.take(12) },
                        subtitle = entry.arguments?.getString("subtitle").orEmpty(),
                    )
                },
                onBack = { nav.popBackStack() },
            )
        }
        composable(Routes.STORAGE, arguments = nodeArguments()) { entry ->
            val addr = entry.arguments?.getString("addr").orEmpty()
            StorageScreen(node = addr, hostname = entry.arguments?.getString("host") ?: addr, onBack = { nav.popBackStack() })
        }
        composable(Routes.RESOURCES, arguments = nodeArguments()) { entry ->
            val addr = entry.arguments?.getString("addr").orEmpty()
            val host = entry.arguments?.getString("host") ?: addr
            ResourceTypesScreen(
                node = addr,
                hostname = host,
                onBack = { nav.popBackStack() },
                onType = { nav.navigate(Routes.resourceList(addr, host, it.namespace, it.type, it.sensitive)) },
            )
        }
        composable(Routes.RESOURCE_LIST, arguments = resourceArguments()) { entry ->
            val addr = entry.arguments?.getString("addr").orEmpty()
            val host = entry.arguments?.getString("host") ?: addr
            val ref = entry.resourceRef(addr)
            ResourceListScreen(
                ref = ref,
                hostname = host,
                onBack = { nav.popBackStack() },
                // The item's own namespace when the list spans several.
                onItem = { nav.navigate(Routes.resource(addr, host, ref.copy(namespace = it.namespace.ifEmpty { ref.namespace }), it.id)) },
            )
        }
        composable(Routes.RESOURCE, arguments = resourceArguments() + navArgument("id") { type = NavType.StringType }) { entry ->
            val addr = entry.arguments?.getString("addr").orEmpty()
            ResourceDetailScreen(
                ref = entry.resourceRef(addr),
                id = entry.arguments?.getString("id").orEmpty(),
                onBack = { nav.popBackStack() },
            )
        }
        composable(Routes.CHANGELOG) { ChangelogScreen(onBack = { nav.popBackStack() }) }
        composable(Routes.LICENSES) { LicensesScreen(onBack = { nav.popBackStack() }) }
        composable(Routes.SUPPORTED_INTEGRATIONS) {
            SupportedIntegrationsScreen(configs = app.configRepository, talos = app.talosRepository, onBack = { nav.popBackStack() })
        }
        composable(Routes.FUNDING) { FundingScreen(onBack = { nav.popBackStack() }) }
        composable(Routes.SUPPORT_BUNDLE) { SupportBundleScreen(onBack = { nav.popBackStack() }) }
        composable(Routes.INTEGRATIONS) { IntegrationsScreen(onBack = { nav.popBackStack() }) }
        composable(
            Routes.DEBUG,
            arguments = listOf(
                navArgument("addr") { type = NavType.StringType },
                navArgument("host") { type = NavType.StringType },
                navArgument("ctx") { type = NavType.StringType; defaultValue = "" },
            ),
        ) { entry ->
            val addr = entry.arguments?.getString("addr").orEmpty()
            val context = entry.arguments?.getString("ctx").orEmpty()
                .ifEmpty { app.configRepository.config.value?.activeContext.orEmpty() }
            DebugShellScreen(
                node = addr,
                hostname = entry.arguments?.getString("host") ?: addr,
                context = context,
                onBack = { nav.popBackStack() },
            )
        }
        composable(
            Routes.EVENTS,
            arguments = listOf(
                navArgument("addr") { type = NavType.StringType; defaultValue = "" },
                navArgument("host") { type = NavType.StringType; defaultValue = "" },
            ),
        ) { entry ->
            val addr = entry.arguments?.getString("addr").orEmpty().ifEmpty { null }
            EventsScreen(
                node = addr,
                hostname = addr?.let { entry.arguments?.getString("host")?.ifEmpty { null } ?: it },
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
        composable(Routes.KUBESPAN) {
            KubeSpanScreen(
                onBack = { nav.popBackStack() },
                // Only a node the talosconfig targets has a detail screen.
                onNode = { n -> if (n.node.isNotBlank()) nav.navigate(Routes.node(n.node, n.hostname, n.role)) },
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
                )
            }
        }
        composable(Routes.NETWORK_POLICIES) { NetworkPoliciesScreen(onBack = { nav.popBackStack() }) }
        composable(Routes.API_HEALTH) { ApiHealthScreen(onBack = { nav.popBackStack() }, onAudit = { nav.navigate(Routes.AUDIT) }) }
        composable(Routes.AUDIT) { AuditScreen(onBack = { nav.popBackStack() }) }
        composable(Routes.CHECKUP) { CheckupScreen(onBack = { nav.popBackStack() }) }
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
            name.levis.ichor.ui.dataservices.DataServicesScreen(initial = kind, onBack = { nav.popBackStack() })
        }
        composable(Routes.ARGO_CD) {
            ArgoAppsScreen(
                onBack = { nav.popBackStack() },
                onApp = { ns, name -> nav.navigate(Routes.argoApp(ns, name)) },
                onWindows = { nav.navigate(Routes.ARGO_WINDOWS) },
            )
        }
        composable(Routes.ARGO_WINDOWS) {
            ArgoWindowsScreen(onBack = { nav.popBackStack() })
        }
        composable(
            Routes.ARGO_APP,
            arguments = listOf(
                navArgument("ns") { type = NavType.StringType; defaultValue = "" },
                navArgument("name") { type = NavType.StringType; defaultValue = "" },
            ),
        ) { entry ->
            ArgoAppScreen(
                namespace = entry.arguments?.getString("ns").orEmpty(),
                name = entry.arguments?.getString("name").orEmpty(),
                onBack = { nav.popBackStack() },
                onNode = { n, tab -> nav.navigate(Routes.node(n.node, n.hostname, n.role, tab)) },
                onWindows = { nav.navigate(Routes.ARGO_WINDOWS) },
            )
        }
        composable(Routes.FLUX) {
            FluxScreen(onBack = { nav.popBackStack() }, onApp = { kind, ns, name -> nav.navigate(Routes.fluxApp(kind, ns, name)) })
        }
        composable(
            Routes.FLUX_APP,
            arguments = listOf(
                navArgument("kind") { type = NavType.StringType; defaultValue = "" },
                navArgument("ns") { type = NavType.StringType; defaultValue = "" },
                navArgument("name") { type = NavType.StringType; defaultValue = "" },
            ),
        ) { entry ->
            val kind = entry.arguments?.getString("kind").orEmpty()
            val ns = entry.arguments?.getString("ns").orEmpty()
            val name = entry.arguments?.getString("name").orEmpty()
            FluxAppScreen(
                kind = kind,
                namespace = ns,
                name = name,
                onBack = { nav.popBackStack() },
                onNode = { n, tab -> nav.navigate(Routes.node(n.node, n.hostname, n.role, tab)) },
                onDiff = { nav.navigate(Routes.fluxDiff(kind, ns, name)) },
            )
        }
        composable(
            Routes.FLUX_DIFF,
            arguments = listOf(
                navArgument("kind") { type = NavType.StringType; defaultValue = "" },
                navArgument("ns") { type = NavType.StringType; defaultValue = "" },
                navArgument("name") { type = NavType.StringType; defaultValue = "" },
            ),
        ) { entry ->
            FluxDiffScreen(
                kind = entry.arguments?.getString("kind").orEmpty(),
                namespace = entry.arguments?.getString("ns").orEmpty(),
                name = entry.arguments?.getString("name").orEmpty(),
                onBack = { nav.popBackStack() },
            )
        }
        composable(Routes.ETCD) { EtcdScreen(onBack = { nav.popBackStack() }) }
        composable(Routes.HEALTH) {
            HealthScreen(onBack = { nav.popBackStack() }, onDiagnose = { note -> nav.navigate(Routes.diagnosis(note)) })
        }
        composable(
            Routes.DIAGNOSIS,
            arguments = listOf(navArgument("note") { type = NavType.StringType; defaultValue = "" }),
        ) { entry ->
            DiagnosisScreen(
                initialNote = entry.arguments?.getString("note").orEmpty(),
                onBack = { nav.popBackStack() },
                onSettings = { nav.navigate(Routes.SETTINGS) },
            )
        }
        composable(Routes.SETTINGS) {
            SettingsScreen(
                configs = app.configRepository,
                uiPreferences = app.uiPreferences,
                appLock = app.appLock,
                talos = app.talosRepository,
                onBack = { nav.popBackStack() },
                onReimport = { nav.navigate(Routes.IMPORT) },
                onIssueConfig = { nav.navigate(Routes.ISSUE_CONFIG) },
                onSupportBundle = { nav.navigate(Routes.SUPPORT_BUNDLE) },
                onIntegrations = { nav.navigate(Routes.INTEGRATIONS) },
                onChangelog = { nav.navigate(Routes.CHANGELOG) },
                onLicenses = { nav.navigate(Routes.LICENSES) },
                onSupportedIntegrations = { nav.navigate(Routes.SUPPORTED_INTEGRATIONS) },
                onFunding = { nav.navigate(Routes.FUNDING) },
                onCleared = {
                    app.launchSync(runNow = true)
                    nav.resetTo(Routes.IMPORT)
                },
            )
        }
    }

    // Its dialogs are windows over the lock screen: kept for after the unlock.
    val locked by app.appLock.locked.collectAsStateWithLifecycle()
    val config by app.configRepository.config.collectAsStateWithLifecycle()
    if (!locked) {
        IncomingBackup(
            uri = incomingBackup,
            hasConfig = config != null,
            onRead = onIncomingBackupRead,
            onRestored = {
                app.launchSync(runNow = true)
                nav.resetTo(Routes.OVERVIEW)
            },
            onConfig = { text ->
                incomingConfig = text
                nav.navigate(Routes.IMPORT) { launchSingleTop = true }
            },
        )
    }
}

/** The addr/host arguments of a per-node screen. */
private fun nodeArguments() = listOf(
    navArgument("addr") { type = NavType.StringType },
    navArgument("host") { type = NavType.StringType },
)

/** The arguments of the resource browser's list and detail screens. */
private fun resourceArguments() = nodeArguments() + listOf(
    navArgument("ns") { type = NavType.StringType; defaultValue = "" },
    navArgument("type") { type = NavType.StringType },
    navArgument("sensitive") { type = NavType.BoolType; defaultValue = false },
)

private fun NavBackStackEntry.resourceRef(addr: String) = ResourceRef(
    node = addr,
    namespace = arguments?.getString("ns").orEmpty(),
    type = arguments?.getString("type").orEmpty(),
    sensitive = arguments?.getBoolean("sensitive") ?: false,
)

/** What a node row's swipe or action sheet leads to, from home or the Nodes screen. */
private fun NavHostController.openNodeAction(n: NodeOverview, action: NodeAction) {
    when (action) {
        NodeAction.LIVE -> navigate(Routes.node(n.node, n.hostname, n.role, tab = 2))
        NodeAction.SERVICES -> navigate(Routes.node(n.node, n.hostname, n.role))
        NodeAction.KERNEL_LOG -> navigate(Routes.logs(n.node, n.hostname, null))
        NodeAction.SHELL -> navigate(Routes.debug(n.node, n.hostname))
        NodeAction.REBOOT -> navigate(Routes.node(n.node, n.hostname, n.role, action = "reboot"))
        NodeAction.SHUTDOWN -> navigate(Routes.node(n.node, n.hostname, n.role, action = "shutdown"))
    }
}

/**
 * The route a share link opens over the overview; null for the overview itself. A node only
 * when it is one of the cluster's (with its role, for the control-plane warnings): a link
 * cannot point the app's Talos calls at another address. A Talos screen on a cluster added
 * from a kubeconfig (another phone's context of the same name) stays on its home.
 */
private suspend fun ShareTarget.route(app: TalosApp): String? = when (target) {
    ShareTarget.ETCD -> Routes.ETCD.takeUnless { app.configRepository.config.value?.activeIsKube == true }
    ShareTarget.HEALTH -> Routes.HEALTH.takeUnless { app.configRepository.config.value?.activeIsKube == true }
    ShareTarget.ARGO_CD -> Routes.ARGO_CD
    ShareTarget.FLUX -> Routes.FLUX
    ShareTarget.NODE -> runCatching { app.talosRepository.overview() }.getOrNull()?.nodes
        ?.firstOrNull { n -> addr.isNotEmpty() && n.node == addr || host.isNotEmpty() && n.hostname == host }
        ?.let { n -> Routes.node(n.node, n.hostname, n.role, nodeTab) }
    ShareTarget.ARGO_APP -> Routes.argoApp(namespace, name)
    ShareTarget.FLUX_APP -> Routes.fluxApp(kind, namespace, name)
    else -> kubeFocus?.let { Routes.workloads(it) }
}

/** Navigates to [route] and drops everything else from the back stack. */

private fun NavHostController.resetTo(route: String) {
    navigate(route) {
        popUpTo(graph.id) { inclusive = true }
        launchSingleTop = true
    }
}
