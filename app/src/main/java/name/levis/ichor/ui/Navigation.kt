package name.levis.ichor.ui

import android.net.Uri
import android.widget.Toast
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.LocalContext
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.navigation.NavHostController
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.rememberNavController
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.activeSummary
import name.levis.ichor.data.activeIsKube
import name.levis.ichor.model.DataServiceKind
import name.levis.ichor.model.Feature
import name.levis.ichor.model.ShareTarget
import name.levis.ichor.model.allows
import name.levis.ichor.model.contextFor
import name.levis.ichor.model.kubeFocus
import name.levis.ichor.model.nodeTab
import name.levis.ichor.ui.backup.IncomingBackup
import name.levis.ichor.ui.changelog.WhatsNewHost
import name.levis.ichor.ui.debug.LiveShell
import name.levis.ichor.ui.importconfig.ImportScreen
import name.levis.ichor.ui.kubebrowser.KubeBrowserRoutes
import name.levis.ichor.ui.kubebrowser.KubeLinks
import name.levis.ichor.ui.overview.OverviewScreen
import name.levis.ichor.ui.share.parseShareLink
import name.levis.ichor.ui.nav.Routes
import name.levis.ichor.ui.nav.appGraph
import name.levis.ichor.ui.nav.gitOpsGraph
import name.levis.ichor.ui.nav.kubeGraph
import name.levis.ichor.ui.nav.openNodeAction
import name.levis.ichor.ui.nav.resetTo
import name.levis.ichor.ui.nav.talosGraph


/** Screens a notification can open directly (see MainActivity.EXTRA_OPEN). */
enum class DeepLink { ISSUE_CONFIG, DEMO, DEMO_KUBE, ARGO_WINDOWS, ARGO_CD, FLUX, CHECKUP }

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
        val key = shell.key
        val args = top?.arguments
        val showing = if (key.isPod) {
            top?.destination?.route == Routes.POD_SHELL && args?.getString("ctx") == key.context &&
                args.getString("ns") == key.namespace && args.getString("pod") == key.pod && args.getString("c").orEmpty() == key.container
        } else {
            top?.destination?.route == Routes.DEBUG && args?.getString("addr") == key.node &&
                args.getString("ctx").orEmpty().ifEmpty { active } == key.context
        }
        val route = if (key.isPod) Routes.podShell(key.context, key.namespace, key.pod, key.container) else Routes.debug(key.node, shell.hostname, key.context)
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

    NavHost(navController = nav, startDestination = if (startWithImport) Routes.IMPORT else Routes.OVERVIEW) {
        talosGraph(nav, app, kubeLinks)
        kubeGraph(nav, app, kubeLinks)
        gitOpsGraph(nav, app, kubeLinks)
        appGraph(nav, app, kubeLinks)
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
                onKubeNodes = { nav.navigate(Routes.kubeNodes(it)) },
                onCheckup = { nav.navigate(Routes.CHECKUP) },
                onApiHealth = { nav.navigate(Routes.API_HEALTH) },
                onNetworkPolicies = { nav.navigate(Routes.NETWORK_POLICIES) },
                onResources = { nav.navigate(KubeBrowserRoutes.KINDS) },
                onHelm = { nav.navigate(KubeBrowserRoutes.HELM) },
                onStorage = { nav.navigate(KubeBrowserRoutes.STORAGE) },
                onDrain = { nav.navigate(Routes.maintenance(it, it, drain = true)) },
                onActivity = { nav.navigate(Routes.activity(it)) },
            )
            // After an update: what changed since the build that ran before.
            WhatsNewHost(onFullChangelog = { nav.navigate(Routes.CHANGELOG) })
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
    ShareTarget.DATA -> Routes.dataServices(DataServiceKind.entries.firstOrNull { it.catalogId == kind })
    ShareTarget.CHECKUP -> Routes.CHECKUP
    else -> kubeFocus?.let { Routes.workloads(it) }
}
