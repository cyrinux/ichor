package name.levis.ichor.ui

import name.levis.ichor.selectCluster
import android.content.Context
import android.net.Uri
import android.widget.Toast
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.LocalContext
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.navigation.NavHostController
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.rememberNavController
import name.levis.ichor.BuildConfig
import name.levis.ichor.ui.nav.TestRoute
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.activeSummary
import name.levis.ichor.data.activeIsKube
import name.levis.ichor.model.DataServiceKind
import name.levis.ichor.model.opensAlone
import name.levis.ichor.model.Feature
import name.levis.ichor.model.KubeFocus
import name.levis.ichor.model.NodeOverview
import name.levis.ichor.model.ShareTarget
import name.levis.ichor.model.allows
import name.levis.ichor.model.configuredNode
import name.levis.ichor.model.contextFor
import name.levis.ichor.model.kubeFocus
import name.levis.ichor.model.kubeNodeName
import name.levis.ichor.model.linkedPlugin
import name.levis.ichor.model.nodeTab
import name.levis.ichor.ui.plugins.pluginRouteFor
import name.levis.ichor.ui.workloads.NETPERF_TAB
import name.levis.ichor.monitor.AlertAction
import name.levis.ichor.monitor.AlertActionRequest
import name.levis.ichor.ui.backup.IncomingBackup
import name.levis.ichor.ui.changelog.WhatsNewHost
import name.levis.ichor.ui.debug.LiveShell
import name.levis.ichor.ui.importconfig.ImportScreen
import name.levis.ichor.ui.kubebrowser.KubeBrowserRoutes
import name.levis.ichor.ui.kubebrowser.KubeLinks
import name.levis.ichor.ui.overview.OverviewScreen
import name.levis.ichor.ui.overview.OverviewScreenNavigation
import name.levis.ichor.ui.share.OpenLink
import name.levis.ichor.ui.share.parseShareLink
import name.levis.ichor.ui.nav.Routes
import name.levis.ichor.ui.nav.WithClusterRail
import name.levis.ichor.ui.nav.appGraph
import name.levis.ichor.ui.nav.gitOpsGraph
import name.levis.ichor.ui.settings.SettingsPage
import name.levis.ichor.ui.nav.kubeGraph
import name.levis.ichor.ui.nav.openNodeAction
import name.levis.ichor.ui.nav.navMaskMark
import name.levis.ichor.ui.nav.rebuildAfterMaskSwitch
import name.levis.ichor.ui.nav.resetTo
import name.levis.ichor.ui.nav.talosGraph
import kotlinx.coroutines.launch


/**
 * Screens a notification or the static launcher shortcut (res/xml/shortcuts.xml) can open
 * directly (see MainActivity.EXTRA_OPEN, which carries the name).
 */
enum class DeepLink {
    ISSUE_CONFIG, DEMO, DEMO_KUBE, ARGO_WINDOWS, ARGO_CD, FLUX, CHECKUP, CONFIG_TRY, SEARCH,

    // The fleet widget (FleetWidget): the fleet screen.
    FLEET,

    // A followed run's notification (OperationService): the screen it shows on.
    UPGRADE, MAINTENANCE, CONFIG_MULTI, CLUSTER_UPGRADE, K8S_UPGRADE, JOIN_NODE, ARGO_APP, FLUX_APP, WORKLOAD, NETPERF, RUNBOOK, CA_ROTATION;

    companion object {
        /** The screen an EXTRA_OPEN value names; null for anything else. */
        fun named(name: String?): DeepLink? = entries.firstOrNull { it.name == name }
    }
}

/**
 * [deepLink]: a screen to open once over the overview; [onDeepLinkHandled] then clears it.
 * [openCluster]: the fingerprint of a cluster to show (a launcher shortcut), cleared by [onClusterOpened].
 * [incomingBackup]: a backup file opened from another app, to restore; cleared by [onIncomingBackupRead].
 * [openShell]: a debug shell to go back to (its notification), cleared by [onShellOpened].
 * [openLink]: a share link, to open on the cluster it names once unlocked, with the confirmation
 * of the action an alert's button asked for there, if any; cleared by [onLinkOpened].
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
    openLink: OpenLink? = null,
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
    // An alert's button only opens the screen's own confirmation: nothing is sent before it.
    LaunchedEffect(openLink, linkLocked) {
        val link = openLink ?: return@LaunchedEffect
        if (linkLocked) return@LaunchedEffect
        val target = parseShareLink(link.url)
        if (target == null) {
            Toast.makeText(context, R.string.share_link_invalid, Toast.LENGTH_LONG).show()
        } else {
            openShareTarget(app, nav, context, target, link.action?.takeIf { it.matches(target) })
        }
        // Last: clearing the link restarts this effect, which would cancel a node lookup.
        onLinkOpened()
    }

    if (BuildConfig.DEBUG) {
        DisposableEffect(nav) {
            TestRoute.nav = nav
            onDispose { TestRoute.nav = null }
        }
    }

    // A search result: opened the way its share link would be.
    val scope = rememberCoroutineScope()
    val openTarget: (ShareTarget) -> Unit = remember(nav) { { target -> scope.launch { openShareTarget(app, nav, context, target) } } }

    // Over whatever is on screen, unless it already is that shell.
    LaunchedEffect(openShell) {
        val shell = openShell ?: return@LaunchedEffect
        val top = nav.currentBackStackEntry
        val active = app.configRepository.config.value?.activeContext.orEmpty()
        val key = shell.key
        val args = top?.arguments
        val showing = if (key.isVM) {
            top?.destination?.route == Routes.VM_CONSOLE && args?.getString("ctx") == key.context &&
                args.getString("ns") == key.namespace && args.getString("vm") == key.vm
        } else if (key.kubeNode) {
            top?.destination?.route == Routes.KUBE_NODE_DEBUG && args?.getString("ctx") == key.context && args.getString("node") == key.node
        } else if (key.isPod) {
            top?.destination?.route == Routes.POD_SHELL && args?.getString("ctx") == key.context &&
                args.getString("ns") == key.namespace && args.getString("pod") == key.pod && args.getString("c").orEmpty() == key.container
        } else {
            top?.destination?.route == Routes.DEBUG && args?.getString("addr") == key.node &&
                args.getString("ctx").orEmpty().ifEmpty { active } == key.context
        }
        val route = when {
            key.kubeNode -> Routes.kubeNodeDebug(key.context, key.node, key.namespace)
            key.isVM -> Routes.vmConsole(key.context, key.namespace, key.vm)
            key.isPod -> Routes.podShell(key.context, key.namespace, key.pod, key.container)
            else -> Routes.debug(key.node, shell.hostname, key.context)
        }
        if (!startWithImport && !showing) nav.navigate(route)
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
            // The try's notification: back on its node's machine config, where the countdown runs.
            DeepLink.CONFIG_TRY -> app.configTryManager.current.value?.takeIf { !startWithImport }?.let {
                nav.navigate(Routes.machineConfig(it.node, it.hostname)) { launchSingleTop = true }
            }
            DeepLink.DEMO_KUBE -> nav.navigate(Routes.DEMO_KUBE) { launchSingleTop = true }
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
            // The launcher's "Search clusters": the last-known data of every cluster, so no role
            // check here (its refresh action asks for one); nothing to search before an import.
            DeepLink.SEARCH -> if (!startWithImport) {
                nav.navigate(Routes.SEARCH) { launchSingleTop = true }
            }
            // The fleet widget: the last background checks only, so no role check either.
            DeepLink.FLEET -> if (!startWithImport) {
                nav.navigate(Routes.FLEET) { launchSingleTop = true }
            }
            // A followed run's notification: back on the screen that shows it, while it is followed.
            DeepLink.UPGRADE -> app.upgradeManager.current.value?.takeIf { !startWithImport }?.let {
                nav.navigate(Routes.upgrade(it.node, it.hostname)) { launchSingleTop = true }
            }
            DeepLink.MAINTENANCE -> app.maintenanceManager.current.value?.takeIf { !startWithImport }?.let {
                nav.navigate(Routes.maintenance(it.node, it.hostname)) { launchSingleTop = true }
            }
            // The node whose machine config screen started the apply; its address names it there.
            DeepLink.CONFIG_MULTI -> app.configMultiManager.current.value?.takeIf { !startWithImport }?.let {
                nav.navigate(Routes.machineConfig(it.origin, it.origin)) { launchSingleTop = true }
            }
            DeepLink.CLUSTER_UPGRADE -> app.clusterUpgradeManager.current.value?.takeIf { !startWithImport }?.let {
                nav.navigate(Routes.clusterUpgrade(it.version)) { launchSingleTop = true }
            }
            DeepLink.K8S_UPGRADE -> if (!startWithImport && app.k8sUpgradeManager.current.value != null) {
                nav.navigate(Routes.K8S_UPGRADE) { launchSingleTop = true }
            }
            DeepLink.JOIN_NODE -> if (!startWithImport && app.joinManager.current.value != null) {
                nav.navigate(Routes.JOIN_NODE) { launchSingleTop = true }
            }
            // The Kubernetes runs: the screen they were started from, while the manager holds them.
            DeepLink.ARGO_APP -> app.gitOpsOperations.argoSync.value?.takeIf { !startWithImport && canUseKube() }?.let {
                nav.navigate(Routes.argoApp(it.namespace, it.name)) { launchSingleTop = true }
            }
            DeepLink.FLUX_APP -> app.gitOpsOperations.fluxReconcile.value?.takeIf { !startWithImport && canUseKube() }?.let {
                nav.navigate(Routes.fluxApp(it.kind, it.namespace, it.name)) { launchSingleTop = true }
            }
            DeepLink.WORKLOAD -> app.rolloutManager.current.value?.takeIf { !startWithImport && canUseKube() }?.let {
                val w = it.workload
                nav.navigate(Routes.workloads(KubeFocus(0, w.key, w.namespace, w.name))) { launchSingleTop = true }
            }
            DeepLink.NETPERF -> if (!startWithImport && canUseKube() && app.netPerfManager.current.value != null) {
                nav.navigate(Routes.workloads(KubeFocus(NETPERF_TAB))) { launchSingleTop = true }
            }
            DeepLink.RUNBOOK -> if (!startWithImport && app.runbookManager.current.value != null) {
                nav.navigate(Routes.RUNBOOK_RUN) { launchSingleTop = true }
            }
            DeepLink.CA_ROTATION -> if (!startWithImport && app.caRotationManager.current.value != null) {
                nav.navigate(Routes.ROTATE_CA) { launchSingleTop = true }
            }
        }
        onDeepLinkHandled()
    }

    // Browser screens a pod's sheet opens: its YAML, a port-forward.
    val kubeLinks = remember(nav) {
        KubeLinks(
            onObject = { nav.navigate(KubeBrowserRoutes.obj(it)) },
            onPortForward = { ns, pod -> nav.navigate(KubeBrowserRoutes.forward(ns, pod)) },
            onShell = { ns, pod, container ->
                val active = app.configRepository.config.value?.activeContext.orEmpty()
                nav.navigate(Routes.podShell(active, ns, pod, container))
            },
        )
    }

    // While screenshot mode switches, every screen is covered: none can draw a name read
    // under the previous setting, nor one half-way through the switch.
    val maskSwitching by app.maskTransition.collectAsStateWithLifecycle()
    // Once switched, every screen but Settings is rebuilt: none keeps what it read before.
    // Also for a switch that ended while this was not composed or the app was in the background.
    val maskEpoch by app.maskSwitcher.epoch.collectAsStateWithLifecycle()
    LaunchedEffect(nav, maskEpoch) {
        if (!navMaskMark.due(maskEpoch)) return@LaunchedEffect
        navMaskMark.done(maskEpoch)
        val start = if (app.configRepository.config.value != null) Routes.OVERVIEW else Routes.IMPORT
        nav.rebuildAfterMaskSwitch(start)
    }
    // On a tablet, the clusters and the cluster's sections in a rail beside the screens.
    WithClusterRail(nav, app) { Box(Modifier.fillMaxSize()) {
        NavHost(navController = nav, startDestination = if (startWithImport) Routes.IMPORT else Routes.OVERVIEW, modifier = Modifier.fillMaxSize()) {
            talosGraph(nav, app, kubeLinks)
            kubeGraph(nav, app, kubeLinks)
            gitOpsGraph(nav, app, kubeLinks)
            appGraph(nav, app, kubeLinks, openTarget)
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
            composable(Routes.DEMO_KUBE) {
                ImportScreen(
                    autoStartDemo = true,
                    kubeDemo = true,
                    onImported = {
                        app.launchSync(runNow = true)
                        nav.resetTo(Routes.OVERVIEW)
                    },
                    onBack = { nav.popBackStack() },
                )
            }
            composable(Routes.OVERVIEW) {
                OverviewScreen(
                    OverviewScreenNavigation(
                        onNode = { nav.navigate(Routes.node(it.node, it.hostname, it.role)) },
                        onNodeAction = nav::openNodeAction,
                        onEtcd = { nav.navigate(Routes.ETCD) },
                        onKubeSpan = { nav.navigate(Routes.KUBESPAN) },
                        onWorkloads = { nav.navigate(Routes.workloads()) },
                        onMetrics = { nav.navigate(Routes.metrics()) },
                        onAlerts = { nav.navigate(Routes.alerts()) },
                        onDataServices = { kind, solo -> nav.navigate(Routes.dataServices(kind, solo)) },
                        onPlugin = { nav.navigate(name.levis.ichor.ui.plugins.pluginRoute(it)) },
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
                        onUpgradeCluster = { version -> nav.navigate(Routes.clusterUpgrade(version)) },
                        onUpgradeKubernetes = { nav.navigate(Routes.K8S_UPGRADE) },
                        onAddNode = { nav.navigate(Routes.JOIN_NODE) },
                        onRunbooks = { nav.navigate(Routes.RUNBOOKS) },
                        onUpdates = { nav.navigate(Routes.UPDATES) },
                        onDiagnose = { nav.navigate(Routes.diagnosis()) },
                        onAddCluster = { nav.navigate(Routes.IMPORT) },
                        onClustersCleared = { nav.resetTo(Routes.IMPORT) },
                        onChangelog = { nav.navigate(Routes.CHANGELOG) },
                        onAllNodes = { nav.navigate(Routes.nodes(it)) },
                        onKubeNodes = { nav.navigate(Routes.kubeNodes(it)) },
                        onCheckup = { nav.navigate(Routes.CHECKUP) },
                        onCapacity = { nav.navigate(Routes.KUBE_CAPACITY) },
                        onApiHealth = { nav.navigate(Routes.API_HEALTH) },
                        onNetworkPolicies = { nav.navigate(Routes.NETWORK_POLICIES) },
                        onPolicies = { nav.navigate(Routes.POLICIES) },
                        onAutoscaling = { nav.navigate(Routes.AUTOSCALING) },
                        onVirtualMachines = { nav.navigate(Routes.VIRTUAL_MACHINES) },
                        onArgoRollouts = { nav.navigate(Routes.ARGO_ROLLOUTS) },
                        onResources = { nav.navigate(KubeBrowserRoutes.KINDS) },
                        onHelm = { nav.navigate(KubeBrowserRoutes.HELM) },
                        onStorage = { nav.navigate(KubeBrowserRoutes.STORAGE) },
                        onServices = { nav.navigate(KubeBrowserRoutes.SERVICES) },
                        onRepository = { nav.navigate(Routes.repoSource()) },
                        onGitHosts = { nav.navigate(Routes.settingsPage(SettingsPage.GIT_HOSTS)) },
                        onCrossplane = { nav.navigate(KubeBrowserRoutes.CROSSPLANE) },
                        onTailscale = { nav.navigate(KubeBrowserRoutes.TAILSCALE) },
                        onClusterLifecycle = { nav.navigate(KubeBrowserRoutes.CLUSTER_LIFECYCLE) },
                        onLoadBalancing = { nav.navigate(KubeBrowserRoutes.LOAD_BALANCING) },
                        onJobs = { nav.navigate(KubeBrowserRoutes.JOBS) },
                        onKubeEvents = { nav.navigate(Routes.KUBE_EVENTS) },
                        onDrain = { nav.navigate(Routes.maintenance(it, it, drain = true)) },
                        onNodeDebug = { nav.navigate(Routes.kubeNodeDebug(app.configRepository.config.value?.activeContext.orEmpty(), it)) },
                        onNodeMeta = { nav.navigate(Routes.nodeMeta(it, it)) },
                        onActivity = { nav.navigate(Routes.activity(it)) },
                        onSearch = { nav.navigate(Routes.SEARCH) { launchSingleTop = true } },
                        onFleet = { nav.navigate(Routes.FLEET) { launchSingleTop = true } },
                        onOpenTarget = openTarget,
                    ),
                )
                // After an update: what changed since the build that ran before.
                WhatsNewHost(onFullChangelog = { nav.navigate(Routes.CHANGELOG) })
            }
        }
        if (maskSwitching) MaskSwitchingPlaceholder()
    } }

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



/**
 * Opens [target] as a share link does: the overview of its cluster (the context on screen when
 * it is one of that cluster), then the screen it names over it, with [action]'s confirmation.
 */
private suspend fun openShareTarget(
    app: TalosApp,
    nav: NavHostController,
    context: Context,
    target: ShareTarget,
    action: AlertActionRequest? = null,
) {
    val stored = app.configRepository.config.value
    val cluster = stored?.summary?.contextFor(target.cluster, stored.activeContext)
    if (cluster == null) {
        Toast.makeText(context, R.string.share_link_unknown_cluster, Toast.LENGTH_LONG).show()
        return
    }
    app.selectCluster(cluster.name)
    nav.resetTo(Routes.OVERVIEW)
    target.route(app, action)?.let { nav.navigate(it) }
}

/**
 * The route a share link opens over the overview; null for the overview itself. A node only
 * when it is one of the cluster's (with its role, for the control-plane warnings): a link
 * cannot point the app's Talos calls at another address. A Talos screen on a cluster added
 * from a kubeconfig (another phone's context of the same name) stays on its home, but a node of
 * it (a node or disk alert) opens its nodes searched for that node. [action]: the
 * confirmation to open there (a reboot, a sync, a reconcile, a 1 h silence), checked to match.
 */
private suspend fun ShareTarget.route(app: TalosApp, action: AlertActionRequest? = null): String? =
    if (app.configRepository.config.value?.activeIsKube == true && (target == ShareTarget.NODE || target == ShareTarget.STORAGE)) {
        kubeNodeRoute(app)
    } else {
        talosRoute(app, action)
    }

private suspend fun ShareTarget.talosRoute(app: TalosApp, action: AlertActionRequest?): String? = when (target) {
    ShareTarget.ETCD -> Routes.ETCD.takeUnless { app.configRepository.config.value?.activeIsKube == true }
    ShareTarget.HEALTH -> Routes.HEALTH.takeUnless { app.configRepository.config.value?.activeIsKube == true }
    ShareTarget.ARGO_CD -> Routes.ARGO_CD
    ShareTarget.FLUX -> Routes.FLUX
    // A reboot confirmation lives on the node screen; a link naming a tab opens the node's tabs.
    ShareTarget.NODE -> clusterNode(app)?.let { n ->
        when {
            action?.action == AlertAction.REBOOT -> Routes.node(n.node, n.hostname, n.role, action = "reboot")
            tab.isNotEmpty() -> Routes.nodeTabs(n.node, n.hostname, nodeTab)
            else -> Routes.node(n.node, n.hostname, n.role)
        }
    }
    ShareTarget.STORAGE -> clusterNode(app)?.let { n -> Routes.storage(n.node, n.hostname) }
    ShareTarget.HARDWARE -> clusterNode(app)?.let { n -> Routes.hardware(n.node, n.hostname) }
    // The screen opens on its map, the first tab.
    ShareTarget.KUBESPAN -> Routes.KUBESPAN.takeUnless { app.configRepository.config.value?.activeIsKube == true }
    ShareTarget.ARGO_APP -> Routes.argoApp(namespace, name, sync = action?.action == AlertAction.SYNC)
    ShareTarget.FLUX_APP -> Routes.fluxApp(kind, namespace, name, reconcile = action?.action == AlertAction.RECONCILE)
    // A data service's catalog id opens its tab; "plugin-<id>" (a plugin finding's alert) where
    // that plugin's tile leads, its own screen when this build cannot tell.
    ShareTarget.DATA -> linkedPlugin?.let { id -> pluginRouteFor(app.pluginRepository, id) ?: KubeBrowserRoutes.plugin(id) }
        ?: DataServiceKind.entries.firstOrNull { it.catalogId == kind }.let { Routes.dataServices(it, solo = it?.opensAlone == true) }
    ShareTarget.CHECKUP -> Routes.CHECKUP
    ShareTarget.ALERTS -> Routes.alerts(silence = action?.takeIf { it.action == AlertAction.SILENCE }?.target.orEmpty())
    else -> kubeFocus?.let { Routes.workloads(it) }
}

/**
 * On a cluster added from a kubeconfig, a node (a node or disk alert) opens its nodes searched
 * for that node; null (its home) when it is none of its.
 */
private suspend fun ShareTarget.kubeNodeRoute(app: TalosApp): String? {
    val names = runCatching { app.kubeRepository.kubeNodes() }.getOrNull()?.nodes.orEmpty().map { it.name }
    return kubeNodeName(names)?.let { Routes.kubeNodes(null, query = it) }
}

/**
 * The cluster's node the link names, by address or hostname; null when it is none of its. When
 * the overview does not have it (it could not be read, say right after a cold start from a
 * notification), a storage or hardware link to a node of the context's config still opens.
 */
private suspend fun ShareTarget.clusterNode(app: TalosApp): NodeOverview? =
    runCatching { app.talosRepository.overview() }.getOrNull()?.nodes
        ?.firstOrNull { n -> addr.isNotEmpty() && n.node == addr || host.isNotEmpty() && n.hostname == host }
        ?: configuredNode(app.configRepository.config.value?.activeSummary)?.let { NodeOverview(it, host.ifEmpty { it }, reachable = false) }
