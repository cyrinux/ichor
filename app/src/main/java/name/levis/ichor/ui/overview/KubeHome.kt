package name.levis.ichor.ui.overview

import name.levis.ichor.ui.components.SMOKE_KUBE_HOME
import androidx.compose.ui.platform.testTag
import name.levis.ichor.selectCluster
import name.levis.ichor.ui.gitops.hasClusterRepo
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import name.levis.ichor.ui.design.IchorCard
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import name.levis.ichor.ui.design.IchorScaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import name.levis.ichor.ui.design.IchorTextButton
import name.levis.ichor.ui.design.IchorTopAppBar
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.BuildConfig
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.KUBE_NODES
import name.levis.ichor.model.ARGO_CD_CATALOG_ID
import name.levis.ichor.model.FLUX_CATALOG_ID
import name.levis.ichor.model.Inventory
import name.levis.ichor.model.ShareTarget
import name.levis.ichor.model.inventoryBadges
import name.levis.ichor.ui.apps.AppsViewModel
import name.levis.ichor.ui.share.ClusterPins
import name.levis.ichor.ui.share.rememberClusterPins
import name.levis.ichor.ui.components.rememberKubeTopNodes
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.data.KubeRepository
import name.levis.ichor.data.activeSummary
import name.levis.ichor.model.ArgoStatus
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.DataServiceKind
import name.levis.ichor.model.ServiceFamily
import name.levis.ichor.model.DataServices
import name.levis.ichor.model.FluxStatus
import name.levis.ichor.model.KubeHomeCard
import name.levis.ichor.model.KubeHomeLayout
import name.levis.ichor.model.KubeNodeInfo
import name.levis.ichor.model.KubeNodesOverview
import name.levis.ichor.model.NodeFilter
import name.levis.ichor.model.PluginDetect
import name.levis.ichor.model.PluginDetectItem
import name.levis.ichor.ui.plugins.PluginDetectViewModel
import name.levis.ichor.model.detected
import name.levis.ichor.model.notReadyNames
import name.levis.ichor.monitor.freezeReminderHook
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.app
import name.levis.ichor.ui.argocd.ArgoViewModel
import name.levis.ichor.ui.history.SinceLastLookedCard
import name.levis.ichor.ui.components.DataFreshness
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.VersionFooter
import name.levis.ichor.ui.components.rememberClusterLabels
import name.levis.ichor.ui.dataservices.DataServicesViewModel
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.flux.FluxViewModel
import name.levis.ichor.ui.kubeauth.GoogleSilentRenewal
import name.levis.ichor.ui.kubeauth.SignInBanner
import name.levis.ichor.ui.kubeauth.SignInSheet
import name.levis.ichor.model.KubeSignInInfo
import name.levis.ichor.model.SignInNeeded
import name.levis.ichor.model.signInNeeded
import androidx.compose.runtime.produceState
import name.levis.ichor.ui.components.pageContent

class KubeHomeViewModel(
    val kube: KubeRepository,
    /** The last background check's nodes, when nothing is cached (see [LoadingViewModel.seed]). */
    private val monitorSeed: () -> TalosRepository.Timed<KubeNodesOverview>? = { null },
) : LoadingViewModel<KubeNodesOverview>() {
    override val keepsDataOnFailure = true
    override fun cached(): TalosRepository.Timed<KubeNodesOverview>? = kube.cached(KUBE_NODES)
    override fun seed(): TalosRepository.Timed<KubeNodesOverview>? = monitorSeed()
    override val restores get() = kube.restores
    override suspend fun fetch() = kube.kubeNodes()
}

/** Where the Kubernetes home leads: Kubernetes screens only, the Talos ones cannot be read. */
class KubeHomeNavigation(
    val onWorkloads: () -> Unit,
    val onMetrics: () -> Unit,
    val onAlerts: () -> Unit,
    val onCheckup: () -> Unit,
    /** Free room per node, workloads by priority, and what losing a node costs. */
    val onCapacity: () -> Unit,
    val onLogSearch: () -> Unit,
    val onApiHealth: () -> Unit,
    val onNetworkPolicies: () -> Unit,
    /** Kyverno's admission policies and their findings. */
    val onPolicies: () -> Unit,
    /** What scales the cluster: HPAs, KEDA, VPA and Karpenter. */
    val onAutoscaling: () -> Unit,
    /** KubeVirt (and Harvester) virtual machines. */
    val onVirtualMachines: () -> Unit,
    /** Argo Rollouts' canaries and blue-greens. */
    val onArgoRollouts: () -> Unit,
    val onArgoCD: () -> Unit,
    val onFlux: () -> Unit,
    /** The Data services screen, on one system's tab (null: the first), or that system alone (solo). */
    val onDataServices: (DataServiceKind?, Boolean) -> Unit,
    /** A CRD plugin's tile: its native screen, its Data services tab or its own screen. */
    val onPlugin: (PluginDetectItem) -> Unit,
    val onSettings: () -> Unit,
    val onFunding: () -> Unit,
    val onAddCluster: () -> Unit,
    val onClustersCleared: () -> Unit,
    val onChangelog: () -> Unit,
    val onResources: () -> Unit,
    val onHelm: () -> Unit,
    /** The PersistentVolumeClaims with their volume, pods and fill. */
    val onStorage: () -> Unit,
    /** The Services with their addresses, ready endpoints and routes. */
    val onServices: () -> Unit,
    /** Crossplane's packages, definitions and resources, problems first. */
    val onCrossplane: () -> Unit,
    /** The Tailscale operator's connectors, proxy groups, proxy classes and DNS, problems first. */
    val onTailscale: () -> Unit,
    /** Cluster API's clusters and machines and Sidero Metal's servers, problems first. */
    val onClusterLifecycle: () -> Unit,
    /** The load balancer controllers, their pools and peers, and why a Service has no address. */
    val onLoadBalancing: () -> Unit,
    /** The cluster's own Git repository, from its root. */
    val onRepository: () -> Unit,
    /** Settings → Git hosts. */
    val onGitHosts: () -> Unit,
    /** The Jobs, failures first. */
    val onJobs: () -> Unit,
    /** The cluster's events, live. */
    val onEvents: () -> Unit,
    /** The drain of a node, by its Kubernetes name. */
    val onDrain: (node: String) -> Unit,
    /** A root shell on a node through a privileged pod, by its Kubernetes name. */
    val onNodeDebug: (node: String) -> Unit,
    /** The taints and labels of a node, by its Kubernetes name. */
    val onNodeMeta: (node: String) -> Unit,
    /** The Kubernetes nodes screen of a large cluster, on one filter (null: all). */
    val onAllNodes: (NodeFilter?) -> Unit,
    /** The action audit log of a cluster, by its context name. */
    val onActivity: (cluster: String) -> Unit,
    /** The search across clusters. */
    val onSearch: () -> Unit,
    /** Every cluster's last check on one list (the fleet view). */
    val onFleet: () -> Unit,
    /** The cluster's runbooks. */
    val onRunbooks: () -> Unit,
    /** The Updates screen: images and charts with a newer version. */
    val onUpdates: () -> Unit,
    /** The apps screen, on those that need a look when [attention]. */
    val onApps: (attention: Boolean) -> Unit,
    /** Opens a pinned object's screen, like its share link. */
    val onOpenTarget: (ShareTarget) -> Unit,
)

/**
 * The home of a cluster added from a kubeconfig, in place of the Talos overview: the API
 * server and who the credentials are, the nodes as Kubernetes lists them, the Kubernetes
 * screens, and Argo CD, Flux and the data services (Longhorn, CloudNativePG...) when installed.
 * No Talos card or action: there is no Talos API; a node can still be cordoned and drained
 * through Kubernetes. The cards and the app bar are arranged like the overview's (a long press
 * on a card, or the menu), in a layout of their own.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun KubeHomeScreen(
    nav: KubeHomeNavigation,
    vm: KubeHomeViewModel = viewModel(factory = factory { KubeHomeViewModel(app.kubeRepository, app::kubeNodesSeed) }),
    argoVm: ArgoViewModel = viewModel(key = "overview-argocd", factory = factory { ArgoViewModel(app.gitOpsRepository, app.kubeRepository, freezeReminderHook(app)) }),
    fluxVm: FluxViewModel = viewModel(key = "overview-flux", factory = factory { FluxViewModel(app.gitOpsRepository, app.kubeRepository) }),
    dataVm: DataServicesViewModel = viewModel(key = "overview-data-services", factory = factory { DataServicesViewModel(app.dataServicesRepository) }),
    crossplaneVm: name.levis.ichor.ui.crossplane.CrossplaneViewModel = viewModel(key = "overview-crossplane", factory = factory { name.levis.ichor.ui.crossplane.CrossplaneViewModel(app.kubeBrowser) }),
    tailscaleVm: name.levis.ichor.ui.tailscale.TailscaleViewModel = viewModel(key = "overview-tailscale", factory = factory { name.levis.ichor.ui.tailscale.TailscaleViewModel(app.kubeBrowser) }),
    lifecycleVm: name.levis.ichor.ui.lifecycle.ClusterLifecycleViewModel = viewModel(key = "overview-cluster-lifecycle", factory = factory { name.levis.ichor.ui.lifecycle.ClusterLifecycleViewModel(app.kubeBrowser) }),
    rolloutsVm: name.levis.ichor.ui.argorollouts.ArgoRolloutsViewModel = viewModel(key = "overview-argo-rollouts", factory = factory { name.levis.ichor.ui.argorollouts.ArgoRolloutsViewModel(app.argoRolloutsRepository) }),
    alertsVm: AlertsCardViewModel = viewModel(key = "overview-alerts", factory = factory { alertsCardViewModel(app) }),
    pluginsVm: PluginDetectViewModel = viewModel(key = "overview-plugins", factory = factory { PluginDetectViewModel(app.pluginRepository) }),
    appsVm: AppsViewModel = viewModel(key = "overview-apps", factory = factory { AppsViewModel(app.talosRepository) }),
) {
    val app = LocalContext.current.applicationContext as TalosApp
    val config by app.configRepository.config.collectAsStateWithLifecycle()
    val generation by app.configRepository.generation.collectAsStateWithLifecycle()
    val invalidations by vm.kube.invalidations.collectAsStateWithLifecycle()
    val state by vm.state.collectAsStateWithLifecycle()
    val clusterColors by app.clusterColors.colors.collectAsStateWithLifecycle()
    val clusterLabels = rememberClusterLabels()
    var showClusters by remember { mutableStateOf(false) }
    var clusterMenu by remember { mutableStateOf(false) }
    var signingIn by remember { mutableStateOf(false) }
    var nodeMenu by remember { mutableStateOf<KubeNodeInfo?>(null) }
    var nodeCordon by remember { mutableStateOf<KubeNodeInfo?>(null) }
    val snackbar = remember { SnackbarHostState() }
    // Long-press a card to arrange them: order, hide, show again; the app bar's actions too.
    val layout by app.uiPreferences.kubeHomeLayout.collectAsStateWithLifecycle()
    val bar by app.uiPreferences.kubeHomeBar.collectAsStateWithLifecycle()
    // Shared with the Talos home: one choice of compact or full nodes for every cluster.
    val nodesExpanded by app.uiPreferences.nodesExpanded.collectAsStateWithLifecycle()
    var customizing by rememberSaveable { mutableStateOf(false) }
    BackHandler(enabled = customizing) { customizing = false }
    val customize = rememberCustomizeTrigger { customizing = true }
    // How the cluster signs in, read again after each sign-in (invalidations): null for static credentials.
    val signIn by produceState<KubeSignInInfo?>(null, config?.activeContext, invalidations) {
        value = config?.activeSummary?.takeIf { it.signIn.isNotEmpty() }?.let { cluster ->
            runCatching { app.kubeAuthRepository.info(cluster.name) }.getOrNull()
        }
    }

    LaunchedEffect(config?.activeContext, invalidations) { vm.refresh(reset = true) }
    // No Talos inventory hints at Argo CD, Flux, Argo Rollouts, the data services or Alertmanager here: all are asked, and
    // answer "not installed" quickly when absent (the operators from the API groups; Garage,
    // which has none, from a listing of the pods).
    val gitopsKey = listOf(config?.activeContext, generation, invalidations)
    LaunchedEffect(gitopsKey) {
        argoVm.load(gitopsKey)
        fluxVm.load(gitopsKey)
        dataVm.load(gitopsKey, hints = "")
        crossplaneVm.refresh(reset = true)
        tailscaleVm.refresh(reset = true)
        lifecycleVm.refresh(reset = true)
        rolloutsVm.refresh(reset = true)
        alertsVm.load(gitopsKey)
        pluginsVm.load(gitopsKey, hints = "")
        // The apps from the pods, as the Talos overview lists them from the nodes' containers.
        appsVm.load(gitopsKey)
    }
    val apps by appsVm.state.collectAsStateWithLifecycle()
    // Credentials that may not list every pod: no apps card rather than an error that stays.
    val appsShown = apps.takeIf { (it as? UiState.Loaded)?.data?.apps?.isNotEmpty() == true }
    val pins = rememberClusterPins()
    val argo by argoVm.state.collectAsStateWithLifecycle()
    val flux by fluxVm.state.collectAsStateWithLifecycle()
    val dataServices by dataVm.state.collectAsStateWithLifecycle()
    val crossplane by crossplaneVm.state.collectAsStateWithLifecycle()
    val tailscale by tailscaleVm.state.collectAsStateWithLifecycle()
    val lifecycle by lifecycleVm.state.collectAsStateWithLifecycle()
    val rollouts by rolloutsVm.state.collectAsStateWithLifecycle()
    val alerts by alertsVm.state.collectAsStateWithLifecycle()
    val argoShown = argo.takeIf { (it as? UiState.Loaded)?.data?.installed == true }
    val fluxShown = flux.takeIf { (it as? UiState.Loaded)?.data?.installed == true }
    val plugins = (pluginsVm.state.collectAsStateWithLifecycle().value as? UiState.Loaded)?.data ?: PluginDetect()
    val dataPlugins = plugins.dataServicePlugins
    // The card lists storage, databases and backups; the other systems are tools in their family.
    val dataShown = dataServices.takeIf { (it as? UiState.Loaded)?.data?.detected(ServiceFamily.DATA)?.isNotEmpty() == true || dataPlugins.isNotEmpty() && it is UiState.Loaded }
    val crossplaneShown = crossplane.takeIf { (it as? UiState.Loaded)?.data?.installed == true }
    val tailscaleShown = tailscale.takeIf { (it as? UiState.Loaded)?.data?.installed == true }
    val lifecycleShown = lifecycle.takeIf { (it as? UiState.Loaded)?.data?.installed == true }
    val rolloutsShown = (rollouts as? UiState.Loaded)?.data?.installed == true
    val alertsShown = alerts.takeIf { it.alertmanagerFound }
    // Cards the cluster has nothing for, left out of the editor too; each offered until its answer came.
    val absentCards = setOfNotNull(
        KubeHomeCard.PINNED.takeIf { pins?.pins.isNullOrEmpty() },
        KubeHomeCard.APPS.takeIf { apps !is UiState.Loading && appsShown == null },
        KubeHomeCard.ARGO_CD.takeIf { argo is UiState.Loaded && argoShown == null },
        KubeHomeCard.FLUX.takeIf { flux is UiState.Loaded && fluxShown == null },
        KubeHomeCard.DATA_SERVICES.takeIf { dataServices is UiState.Loaded && dataShown == null },
        KubeHomeCard.ALERTS.takeIf { alerts is UiState.Loaded && alertsShown == null },
    )

    IchorScaffold(
        modifier = Modifier.testTag(SMOKE_KUBE_HOME),
        snackbarHost = { SnackbarHost(snackbar) },
        bottomBar = { DataFreshness(state) },
        topBar = {
            if (customizing) IchorTopAppBar(
                title = { Text(stringResource(R.string.kube_home_edit_title)) },
                actions = { IchorTextButton(onClick = { customizing = false }) { Text(stringResource(R.string.overview_edit_done)) } },
            ) else IchorTopAppBar(
                modifier = Modifier.clusterSwipe(config, app::selectCluster),
                title = {
                    Box {
                        ClusterTitle(config, clusterColors, clusterLabels, onOpen = { clusterMenu = true }) { ScreenshotModeIcon() }
                        config?.let { stored ->
                            ClusterMenu(
                                expanded = clusterMenu,
                                config = stored,
                                colors = clusterColors,
                                labels = clusterLabels,
                                onSelect = app::selectCluster,
                                onManage = { showClusters = true },
                                onFleet = nav.onFleet,
                                onDismiss = { clusterMenu = false },
                                onRunbooks = nav.onRunbooks,
                                onUpdates = nav.onUpdates,
                            )
                        }
                    }
                },
                actions = {
                    KubeHomeActions(bar, nav, argo = argoShown != null, flux = fluxShown != null, crossplane = crossplaneShown != null, tailscale = tailscaleShown != null, clusterLifecycle = lifecycleShown != null, repository = hasClusterRepo(), onCustomize = { customizing = true })
                },
            )
        },
    ) { padding ->
        config?.takeIf { showClusters }?.let { stored ->
            ManageClustersSheet(
                config = stored,
                colors = clusterColors,
                labels = clusterLabels,
                onClose = { showClusters = false },
                onAddCluster = nav.onAddCluster,
                onClustersCleared = nav.onClustersCleared,
                onActivity = { nav.onActivity(it.name) },
                onGitHosts = nav.onGitHosts,
                onFleet = nav.onFleet,
            )
        }
        val refresh = {
            vm.refresh()
            argoVm.refresh()
            fluxVm.refresh()
            dataVm.refresh()
            alertsVm.refresh()
            pluginsVm.refresh()
        }
        val banner = kubeSignInBanner(signIn, (state as? UiState.Failed)?.message ?: (state as? UiState.Loaded)?.error)
        config?.activeContext?.let { name ->
            if (signingIn) SignInSheet(context = name, onDismiss = { signingIn = false }, onSignedIn = { signingIn = false })
            // An expired "Sign in with Google" token renews by itself; the banner is the fallback.
            GoogleSilentRenewal(name, banner?.needed, onRenewed = refresh)
        }
        if (customizing) HomeEditor(
            layout = layout,
            onChange = app.uiPreferences::setKubeHomeLayout,
            look = kubeHomeCardLook,
            bar = bar,
            barLook = kubeHomeActionLook,
            onBarChange = app.uiPreferences::setKubeHomeBar,
            modifier = Modifier.pageContent(padding),
            absent = absentCards,
        )
        else when (val s = state) {
            UiState.Loading -> LoadingBox(Modifier.pageContent(padding))
            else -> PullToRefreshBox(
                isRefreshing = (s as? UiState.Loaded)?.refreshing == true,
                onRefresh = refresh,
                modifier = Modifier.pageContent(padding).fillMaxSize(),
            ) {
                KubeHomeList(
                    cluster = config?.activeSummary,
                    name = config?.activeSummary?.let(clusterLabels::of),
                    nodes = (s as? UiState.Loaded)?.data,
                    failure = (s as? UiState.Failed)?.message,
                    signIn = banner,
                    onSignIn = { signingIn = true },
                    onRetry = vm::refresh,
                    argo = argoShown,
                    flux = fluxShown,
                    dataServices = dataShown,
                    toolServices = (dataServices as? UiState.Loaded)?.data,
                    plugins = plugins,
                    crossplane = crossplaneShown != null,
                    tailscale = tailscaleShown != null,
                    clusterLifecycle = lifecycleShown != null,
                    argoRollouts = rolloutsShown,
                    alerts = alertsShown,
                    cardRetry = OverviewCardRetry(argo = argoVm::refresh, flux = fluxVm::refresh, alerts = alertsVm::refresh),
                    // A skeleton while the pods are listed, as on the Talos overview.
                    apps = if (apps is UiState.Loading) apps else appsShown,
                    pins = pins,
                    onNode = { nodeMenu = it },
                    nodeRowActions = KubeNodeRowActions(onCordon = { nodeCordon = it }, onDrain = nav.onDrain, onMeta = nav.onNodeMeta, onDebug = nav.onNodeDebug),
                    nodesExpanded = nodesExpanded,
                    onToggleNodes = { app.uiPreferences.setNodesExpanded(!nodesExpanded) },
                    nav = nav,
                    layout = layout,
                    onCustomize = customize,
                )
            }
        }
    }
    KubeNodeActionSheets(nodeMenu, nodeCordon, { nodeCordon = it }, snackbar, onClose = { nodeMenu = null }, onDrain = nav.onDrain, onDebug = nav.onNodeDebug, onCordoned = vm::refresh, onMeta = nav.onNodeMeta)
}

@Composable
private fun KubeHomeList(
    cluster: ContextSummary?,
    name: String?,
    nodes: KubeNodesOverview?,
    failure: UiText?,
    signIn: KubeSignInBanner?,
    onSignIn: () -> Unit,
    onRetry: () -> Unit,
    argo: UiState<ArgoStatus>?,
    flux: UiState<FluxStatus>?,
    dataServices: UiState<DataServices>?,
    /** The systems read with Data services, for those the tools card files in another family. */
    toolServices: DataServices?,
    plugins: PluginDetect,
    crossplane: Boolean,
    tailscale: Boolean,
    clusterLifecycle: Boolean,
    argoRollouts: Boolean,
    alerts: UiState<AlertsOverview?>?,
    cardRetry: OverviewCardRetry,
    /** The apps from the pods; null when the credentials may not list them. */
    apps: UiState<Inventory>?,
    pins: ClusterPins?,
    onNode: (KubeNodeInfo) -> Unit,
    nodeRowActions: KubeNodeRowActions,
    nodesExpanded: Boolean,
    onToggleNodes: () -> Unit,
    nav: KubeHomeNavigation,
    layout: KubeHomeLayout,
    onCustomize: () -> Unit,
) {
    // Offered once, after the first real cluster: first, above everything else.
    val offerAlerts = rememberAlertsOffer(isDemo = cluster?.demo != false)
    // The likely cause of the data services' problems: a node that is not ready.
    val downNodes = remember(nodes) { nodes?.notReadyNames().orEmpty() }
    // The apps' icons on the data services and GitOps cards, as on the Talos overview.
    val inventory = (apps as? UiState.Loaded)?.data
    val appsById = remember(inventory) { inventory?.apps.orEmpty().associateBy { it.id } }
    // Read again with every refresh of the list, once for the summary and the nodes cards.
    val top = rememberKubeTopNodes(nodes?.takeIf { !it.forbidden && it.nodes.isNotEmpty() })
    LazyColumn(
        contentPadding = PaddingValues(16.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
        modifier = Modifier.fillMaxSize(),
    ) {
        if (offerAlerts) item(key = "alerts-offer") { AlertsOfferCard() }
        if (cluster?.demo == true) item(key = "demo") {
            IchorCard(Modifier.fillMaxWidth()) {
                Text(stringResource(R.string.demo_notice), modifier = Modifier.padding(16.dp), style = MaterialTheme.typography.bodyMedium)
            }
        }
        if (BuildConfig.SELF_UPDATE) item { UpdateBanner(onClick = nav.onSettings) } else item { StoreUpdateBanner() }
        if (BuildConfig.DONATIONS || BuildConfig.FEATURE_FUNDING) item { SupportCard(nav.onFunding) }
        cluster?.let { item { KubeCredentialsBanner(it) } }
        signIn?.let { item(key = "sign-in") { SignInBanner(it.method, it.needed, onSignIn) } }
        // The banner says it all when the call failed for want of a sign-in.
        failure?.takeIf { signIn?.needed == null }?.let { item(key = "failure") { KubeUnreachableCard(it, onRetry) } }
        cluster?.fingerprint?.takeIf { it.isNotBlank() }?.let { item(key = "since") { SinceLastLookedCard(it) } }
        // The cards, as arranged; a long press on one opens the arrangement.
        layout.visible.forEach { card ->
            when (card) {
                KubeHomeCard.PINNED -> if (pins != null && pins.pins.isNotEmpty()) item(key = card.name) {
                    // No Talos nodes here: the pinned nodes show without their state.
                    PinnedCard(
                        pins.pins,
                        PinnedData(emptyList(), (argo as? UiState.Loaded)?.data, (flux as? UiState.Loaded)?.data),
                        onOpen = { nav.onOpenTarget(it.target) },
                        onUnpin = { pins.toggle(it.target, it.label) },
                        // Only on the title: a long press on a row offers Unpin.
                        titleModifier = Modifier.longPressToCustomize(onCustomize),
                    )
                }
                KubeHomeCard.APPS -> if (apps != null) item(key = card.name) {
                    val argoData = (argo as? UiState.Loaded)?.data
                    val badges = remember(argoData, inventory) { if (argoData != null && inventory != null) argoData.inventoryBadges(inventory.apps) else emptyMap() }
                    Box(Modifier.longPressToCustomize(onCustomize)) { AppsCard(apps, onOpen = { nav.onApps(false) }, argoBadges = badges, onAttention = { nav.onApps(true) }) }
                }
                KubeHomeCard.SUMMARY -> if (cluster != null) item(key = card.name) {
                    Box(Modifier.longPressToCustomize(onCustomize)) { KubeSummaryCard(name ?: cluster.name, cluster, nodes, top, onCheckup = nav.onCheckup, onCapacity = nav.onCapacity) }
                }
                KubeHomeCard.NODES -> if (nodes != null) item(key = card.name) {
                    Box(Modifier.longPressToCustomize(onCustomize)) { KubeNodesCard(nodes, top, nodesExpanded, onToggleNodes, onNode, nav.onAllNodes, nodeRowActions) }
                }
                KubeHomeCard.TOOLS -> item(key = card.name) {
                    Box(Modifier.longPressToCustomize(onCustomize)) { KubeToolsCard(nav, crossplane = crossplane, tailscale = tailscale, clusterLifecycle = clusterLifecycle, argoRollouts = argoRollouts, plugins = plugins.homeTiles, dataServices = toolServices) }
                }
                KubeHomeCard.DATA_SERVICES -> if (dataServices != null) item(key = card.name) {
                    Box(Modifier.longPressToCustomize(onCustomize)) {
                        DataServicesCard(dataServices, hinted = emptyList(), apps = appsById, onOpen = { nav.onDataServices(it, false) }, plugins = plugins.dataServicePlugins, onPlugin = nav.onPlugin, families = setOf(ServiceFamily.DATA))
                    }
                }
                KubeHomeCard.ARGO_CD -> if (argo != null) item(key = card.name) {
                    Box(Modifier.longPressToCustomize(onCustomize)) { ArgoCard(argo, argoTile = appsById[ARGO_CD_CATALOG_ID], downNodes = downNodes, onOpen = nav.onArgoCD, onRetry = cardRetry.argo) }
                }
                KubeHomeCard.FLUX -> if (flux != null) item(key = card.name) {
                    Box(Modifier.longPressToCustomize(onCustomize)) { FluxCard(flux, fluxTile = appsById[FLUX_CATALOG_ID], onOpen = nav.onFlux, onRetry = cardRetry.flux) }
                }
                KubeHomeCard.ALERTS -> if (alerts != null) item(key = card.name) {
                    Box(Modifier.longPressToCustomize(onCustomize)) { AlertsCard(alerts, onOpen = nav.onAlerts, onRetry = cardRetry.alerts) }
                }
            }
        }
        if (layout.visible.isEmpty()) item(key = "all-hidden") {
            IchorCard(onClick = onCustomize, modifier = Modifier.fillMaxWidth()) {
                MutedText(stringResource(R.string.overview_edit_all_hidden), modifier = Modifier.padding(16.dp))
            }
        }
        item(key = "version") { VersionFooter(onClick = nav.onChangelog) }
    }
}

/** The sign-in banner of the kube home: the [method], and what a failed call said ([needed]). */
internal data class KubeSignInBanner(val method: String, val needed: SignInNeeded?)

/**
 * The banner to show, if any: the cluster was never signed in ([info]), or a call failed
 * asking for a sign-in ([failure], the core's message).
 */
internal fun kubeSignInBanner(info: KubeSignInInfo?, failure: UiText?): KubeSignInBanner? {
    val needed = when (failure) {
        is UiText.SignInRequired -> SignInNeeded(failure.method, failure.reason)
        is UiText.Raw -> signInNeeded(failure.text)
        else -> null
    }
    return when {
        needed != null -> KubeSignInBanner(info?.method ?: needed.method, needed)
        info != null && !info.signedIn -> KubeSignInBanner(info.method, null)
        else -> null
    }
}
