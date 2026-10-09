package name.levis.ichor.ui.overview

import name.levis.ichor.BuildConfig
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.VersionFooter
import name.levis.ichor.ui.components.rememberClusterLabels
import android.widget.Toast
import androidx.activity.compose.BackHandler
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import name.levis.ichor.R
import name.levis.ichor.monitor.freezeReminderHook
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.AutoAwesome
import androidx.compose.material.icons.outlined.VisibilityOff
import androidx.compose.material3.Card
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SmallFloatingActionButton
import androidx.compose.material3.SnackbarDuration
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.SnackbarResult
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import androidx.compose.ui.platform.LocalContext
import name.levis.ichor.TalosApp
import androidx.compose.material3.TextButton
import name.levis.ichor.data.ConfigRepository
import name.levis.ichor.data.SPONSOR_URL
import name.levis.ichor.ui.settings.openUrl
import name.levis.ichor.data.activeSummary
import name.levis.ichor.model.isDemo
import name.levis.ichor.model.Feature
import name.levis.ichor.model.allows
import name.levis.ichor.model.needsEndpoint
import name.levis.ichor.update.StoreUpdateState
import name.levis.ichor.update.UpdateState
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.data.activeIsKube
import name.levis.ichor.data.TOPOLOGY
import name.levis.ichor.model.ClusterTopology
import name.levis.ichor.model.NodeFilter
import name.levis.ichor.model.groupNodes
import name.levis.ichor.data.OVERVIEW
import name.levis.ichor.model.ClusterOverview
import name.levis.ichor.model.OverviewCard
import name.levis.ichor.model.talosUpdateOffer
import name.levis.ichor.model.OverviewLayout
import name.levis.ichor.model.ClusterTime
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.monitor.CERT_WARN_DAYS
import name.levis.ichor.util.daysUntil
import name.levis.ichor.model.NodeOverview
import name.levis.ichor.model.clusterSummary
import name.levis.ichor.model.outage
import name.levis.ichor.model.ClusterOutage
import name.levis.ichor.model.hasLastKnown
import name.levis.ichor.model.lacksPublicIps
import name.levis.ichor.model.health
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.DataFreshness
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.userMessage
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlin.time.Duration.Companion.seconds
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.repeatOnLifecycle
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.apps.AppsViewModel
import name.levis.ichor.model.Inventory
import name.levis.ichor.model.DataServiceKind
import name.levis.ichor.model.DataServices
import name.levis.ichor.model.dataServiceHints
import name.levis.ichor.model.ARGO_CD_CATALOG_ID
import name.levis.ichor.model.ArgoStatus
import name.levis.ichor.model.hasArgoCD
import name.levis.ichor.model.FLUX_CATALOG_ID
import name.levis.ichor.model.FluxStatus
import name.levis.ichor.model.hasFlux
import name.levis.ichor.ui.flux.FluxViewModel
import name.levis.ichor.model.inventoryBadges
import name.levis.ichor.ui.argocd.ArgoViewModel
import name.levis.ichor.ui.dataservices.DataServicesViewModel
import name.levis.ichor.ui.dataservices.downHostnames
import name.levis.ichor.ui.components.pageContent

class OverviewViewModel(
    val talos: TalosRepository,
    val configs: ConfigRepository,
) : LoadingViewModel<ClusterOverview>() {
    override val keepsDataOnFailure = true
    override fun cached(): TalosRepository.Timed<ClusterOverview>? = talos.cached(OVERVIEW)
    override val restores get() = talos.restores
    override suspend fun fetch() = talos.overview()
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun OverviewScreen(
    onNode: (NodeOverview) -> Unit,
    onNodeAction: (NodeOverview, NodeAction) -> Unit,
    onEtcd: () -> Unit,
    onKubeSpan: () -> Unit,
    onWorkloads: () -> Unit,
    onMetrics: () -> Unit,
    onDataServices: (DataServiceKind?) -> Unit,
    onArgoCD: () -> Unit,
    onFlux: () -> Unit,
    onHealth: () -> Unit,
    onEvents: () -> Unit,
    onInsights: () -> Unit,
    onApps: (attention: Boolean) -> Unit,
    onSettings: () -> Unit,
    onFunding: () -> Unit,
    onIssueConfig: () -> Unit,
    onUpgrade: (NodeOverview, String) -> Unit,
    onDiagnose: () -> Unit,
    onAddCluster: () -> Unit,
    onClustersCleared: () -> Unit,
    onChangelog: () -> Unit,
    onAllNodes: (NodeFilter?) -> Unit,
    /** The Kubernetes nodes screen of a cluster added from a kubeconfig, on one filter (null: all). */
    onKubeNodes: (NodeFilter?) -> Unit,
    onCheckup: () -> Unit,
    onApiHealth: () -> Unit,
    onNetworkPolicies: () -> Unit,
    onResources: () -> Unit,
    onHelm: () -> Unit,
    onStorage: () -> Unit,
    onServices: () -> Unit,
    onDrain: (node: String) -> Unit,
    onNodeDebug: (node: String) -> Unit,
    /** The action audit log of a cluster, by its context name. */
    onActivity: (cluster: String) -> Unit,
    vm: OverviewViewModel = viewModel(factory = factory { OverviewViewModel(app.talosRepository, app.configRepository) }),
    timeVm: ClusterTimeViewModel = viewModel(factory = factory { ClusterTimeViewModel(app.talosRepository) }),
    liveVm: ClusterLiveViewModel = viewModel(factory = factory { ClusterLiveViewModel(app.talosRepository) }),
    discoveryVm: NodeDiscoveryViewModel = viewModel(factory = factory { NodeDiscoveryViewModel(app.talosRepository) }),
    appsVm: AppsViewModel = viewModel(key = "overview-apps", factory = factory { AppsViewModel(app.talosRepository) }),
    dataVm: DataServicesViewModel = viewModel(key = "overview-data-services", factory = factory { DataServicesViewModel(app.dataServicesRepository) }),
    argoVm: ArgoViewModel = viewModel(key = "overview-argocd", factory = factory { ArgoViewModel(app.gitOpsRepository, app.kubeRepository, freezeReminderHook(app)) }),
    fluxVm: FluxViewModel = viewModel(key = "overview-flux", factory = factory { FluxViewModel(app.gitOpsRepository, app.kubeRepository) }),
) {
    // A cluster added from a kubeconfig has no Talos overview: its Kubernetes home instead,
    // before any of the Talos loads below start.
    val shown by vm.configs.config.collectAsStateWithLifecycle()
    if (shown?.activeIsKube == true) {
        KubeHomeScreen(
            KubeHomeNavigation(
                onWorkloads = onWorkloads,
                onMetrics = onMetrics,
                onCheckup = onCheckup,
                onApiHealth = onApiHealth,
                onNetworkPolicies = onNetworkPolicies,
                onArgoCD = onArgoCD,
                onFlux = onFlux,
                onDataServices = onDataServices,
                onSettings = onSettings,
                onFunding = onFunding,
                onAddCluster = onAddCluster,
                onClustersCleared = onClustersCleared,
                onChangelog = onChangelog,
                onResources = onResources,
                onHelm = onHelm,
                onStorage = onStorage,
                onServices = onServices,
                onDrain = onDrain,
                onNodeDebug = onNodeDebug,
                onAllNodes = onKubeNodes,
                onActivity = onActivity,
            ),
        )
        return
    }
    val state by vm.state.collectAsStateWithLifecycle()
    val context = LocalContext.current
    val app = context.applicationContext as TalosApp
    val ai by app.aiPreferences.settings.collectAsStateWithLifecycle()
    val clusterColors by app.clusterColors.colors.collectAsStateWithLifecycle()
    val clusterLabels = rememberClusterLabels()
    val scope = rememberCoroutineScope()
    var showClusters by remember { mutableStateOf(false) }
    var clusterMenu by remember { mutableStateOf(false) }
    // The cluster whose endpoints are being edited, and the network search for clusters' nodes.
    var editingEndpoints by remember { mutableStateOf<String?>(null) }
    var scanningEndpoints by remember { mutableStateOf(false) }
    val timeState by timeVm.state.collectAsStateWithLifecycle()
    val config by vm.configs.config.collectAsStateWithLifecycle()
    val invalidations by vm.talos.invalidations.collectAsStateWithLifecycle()

    // Reload whenever the active context changes or cached data was dropped (including first composition).
    LaunchedEffect(config?.activeContext, invalidations) {
        vm.refresh(reset = true)
        timeVm.refresh(reset = true)
    }
    // Once per cluster and config generation (adding nodes or new credentials keep the context):
    // listing every node's containers is too heavy to repeat on each visit.
    val generation by vm.configs.generation.collectAsStateWithLifecycle()
    LaunchedEffect(config?.activeContext, generation, invalidations) {
        appsVm.load(Triple(config?.activeContext, generation, invalidations))
    }
    val apps by appsVm.state.collectAsStateWithLifecycle()

    // Longhorn, Garage, CloudNativePG: only asked (through Kubernetes) when the inventory shows
    // one of them and the role may use the Kubernetes API; other clusters pay nothing.
    val dataHints = (apps as? UiState.Loaded)?.data?.dataServiceHints().orEmpty()
        .takeIf { config?.activeSummary?.allows(Feature.WORKLOADS) == true }.orEmpty()
    LaunchedEffect(config?.activeContext, generation, invalidations, dataHints) {
        // Another cluster without hints: forgotten, so coming back loads again instead of showing the old result.
        if (dataHints.isNotEmpty()) dataVm.load(listOf(config?.activeContext, generation, invalidations, dataHints), dataHints) else dataVm.forget()
    }
    val dataServices by dataVm.state.collectAsStateWithLifecycle()
    // Argo CD likewise, when the inventory shows it.
    val argoHinted = config?.activeSummary?.allows(Feature.WORKLOADS) == true &&
        (apps as? UiState.Loaded)?.data?.hasArgoCD == true
    LaunchedEffect(config?.activeContext, generation, invalidations, argoHinted) {
        if (argoHinted) argoVm.load(listOf(config?.activeContext, generation, invalidations)) else argoVm.forget()
    }
    val argo by argoVm.state.collectAsStateWithLifecycle()
    // Flux likewise.
    val fluxHinted = config?.activeSummary?.allows(Feature.WORKLOADS) == true &&
        (apps as? UiState.Loaded)?.data?.hasFlux == true
    LaunchedEffect(config?.activeContext, generation, invalidations, fluxHinted) {
        if (fluxHinted) fluxVm.load(listOf(config?.activeContext, generation, invalidations)) else fluxVm.forget()
    }
    val flux by fluxVm.state.collectAsStateWithLifecycle()
    // Cards the cluster has nothing for, left out of the editor too; all offered until the inventory is known.
    val absentCards = if (apps is UiState.Loaded) setOfNotNull(
        OverviewCard.DATA_SERVICES.takeIf { dataHints.isEmpty() },
        OverviewCard.ARGO_CD.takeIf { !argoHinted },
        OverviewCard.FLUX.takeIf { !fluxHinted },
    ) else emptySet()

    val lifecycle = LocalLifecycleOwner.current.lifecycle
    // No node answered (VPN off, another network): one notice instead of a list of red nodes.
    val outage = (state as? UiState.Loaded)?.data?.outage
    var showNodesAnyway by remember(config?.activeContext) { mutableStateOf(false) }
    LaunchedEffect(outage != null, config?.activeContext, invalidations) {
        if (outage == null) return@LaunchedEffect
        showNodesAnyway = false
        lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) {
            while (true) {
                delay(UNREACHABLE_RETRY_SECONDS.seconds)
                if ((vm.state.value as? UiState.Loaded)?.refreshing != true) vm.refresh()
            }
        }
    }
    // A new network (VPN connected, back on Wi-Fi) is the likely fix: try again at once.
    // Only while on screen; a change made in the background is caught up on return.
    LaunchedEffect(Unit) {
        var seen = app.vpn.networkChanges.value
        lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) {
            app.vpn.networkChanges.collect { change ->
                if (change == seen) return@collect
                seen = change
                val current = vm.state.value
                val loaded = current as? UiState.Loaded
                if (current is UiState.Failed || loaded?.error != null || loaded?.data?.outage != null) vm.refresh()
            }
        }
    }

    val liveEnabled by app.uiPreferences.liveClusterStats.collectAsStateWithLifecycle()
    val liveState by liveVm.state.collectAsStateWithLifecycle()
    // Live stats and discovery only make sense once a node answers.
    val loaded = state is UiState.Loaded && outage == null
    // Live CPU and memory only while the overview is on screen, once it loaded, and if not turned off.
    // Fewer samples on a large cluster: each one asks every node.
    val nodeCount = (state as? UiState.Loaded)?.data?.nodes?.size ?: 0
    LaunchedEffect(liveEnabled, loaded, config?.activeContext, invalidations, nodeCount) {
        if (!liveEnabled) liveVm.clear()
        if (!liveEnabled || !loaded) return@LaunchedEffect
        lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) { liveVm.poll(config?.activeContext to invalidations, nodeCount) }
    }

    // Members the talosconfig misses, once the overview loaded (its nodes answer, so will discovery).
    val discovered by discoveryVm.offer.collectAsStateWithLifecycle()
    // Long-press a card to arrange them: order, hide, show again.
    val layout by app.uiPreferences.overviewLayout.collectAsStateWithLifecycle()
    val nodesExpanded by app.uiPreferences.nodesExpanded.collectAsStateWithLifecycle()
    val bar by app.uiPreferences.overviewBar.collectAsStateWithLifecycle()
    var customizing by rememberSaveable { mutableStateOf(false) }
    BackHandler(enabled = customizing) { customizing = false }
    val customize = rememberCustomizeTrigger { customizing = true }
    var showDiscovered by remember { mutableStateOf(false) }
    LaunchedEffect(loaded, config?.activeContext, invalidations) {
        if (loaded) discoveryVm.discover() else discoveryVm.clear()
    }
    // The Talos release each cluster's update card was skipped for; the snackbar takes it back.
    val skippedTalosUpdates by app.skippedTalosUpdates.versions.collectAsStateWithLifecycle()
    val snackbar = remember { SnackbarHostState() }
    val skipTalosUpdate: (String) -> Unit = { version ->
        config?.activeSummary?.fingerprint?.let { fingerprint ->
            val before = skippedTalosUpdates[fingerprint]
            app.skippedTalosUpdates.set(fingerprint, version)
            scope.launch {
                // Not queued behind another cluster's: its undo would come too late.
                snackbar.currentSnackbarData?.dismiss()
                val result = snackbar.showSnackbar(
                    context.getString(R.string.overview_talos_skipped, version),
                    actionLabel = context.getString(R.string.common_undo),
                    duration = SnackbarDuration.Long,
                )
                if (result == SnackbarResult.ActionPerformed) app.skippedTalosUpdates.set(fingerprint, before)
            }
        }
    }

    Scaffold(
        bottomBar = { DataFreshness(state) },
        snackbarHost = { SnackbarHost(snackbar) },
        // The AI diagnosis is optional: no trace of it unless it was turned on in Settings.
        floatingActionButton = {
            if (ai.enabled && !customizing) {
                SmallFloatingActionButton(onClick = onDiagnose) {
                    Icon(Icons.Outlined.AutoAwesome, stringResource(R.string.ai_title))
                }
            }
        },
        topBar = {
            if (customizing) TopAppBar(
                title = { Text(stringResource(R.string.overview_edit_title)) },
                actions = { TextButton(onClick = { customizing = false }) { Text(stringResource(R.string.overview_edit_done)) } },
            ) else TopAppBar(
                // Swipe the bar sideways for the previous/next cluster, tap the title for the menu.
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
                                onDismiss = { clusterMenu = false },
                            )
                        }
                    }
                },
                actions = {
                    OverviewActions(
                        bar = bar,
                        nav = OverviewNavigation(
                            onHealth = onHealth,
                            onEvents = onEvents,
                            onWorkloads = onWorkloads,
                            onGitOps = if (argoHinted) onArgoCD else onFlux,
                            onMetrics = onMetrics,
                            onKubeSpan = onKubeSpan,
                            onEtcd = onEtcd,
                            onSettings = onSettings,
                        ),
                        reachable = (state as? UiState.Loaded)?.data?.nodes?.filter { it.reachable }?.map { it.node },
                        health = config?.activeSummary?.allows(Feature.HEALTH) == true,
                        workloads = config?.activeSummary?.allows(Feature.WORKLOADS) == true,
                        gitOps = argoHinted || fluxHinted,
                        onCustomize = { customizing = true },
                    )
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
                onAddCluster = onAddCluster,
                onClustersCleared = onClustersCleared,
                onEndpoints = { editingEndpoints = it.name },
                onActivity = { onActivity(it.name) },
            )
        }
        config?.let { stored ->
            EndpointTools(
                config = stored,
                labels = clusterLabels,
                editing = editingEndpoints,
                scanning = scanningEndpoints,
                onEdit = { editingEndpoints = it },
                onScan = { scanningEndpoints = it },
            )
        }
        config?.activeContext?.takeIf { showDiscovered && discovered.isNotEmpty() }?.let { contextName ->
            AddDiscoveredNodesDialog(
                nodes = discovered,
                onAdd = { nodes ->
                    showDiscovered = false
                    scope.launch {
                        runCatching { app.addClusterNodes(contextName, nodes.map { it.address }) }.fold(
                            onSuccess = {
                                val text = context.resources.getQuantityString(R.plurals.discover_nodes_added, nodes.size, nodes.size)
                                Toast.makeText(context, text, Toast.LENGTH_SHORT).show()
                            },
                            onFailure = { Toast.makeText(context, it.userMessage(), Toast.LENGTH_LONG).show() },
                        )
                    }
                },
                onNotNow = {
                    showDiscovered = false
                    discoveryVm.dismiss(discovered)
                },
                onDismiss = { showDiscovered = false },
            )
        }
        if (customizing) HomeEditor(
            layout = layout,
            onChange = app.uiPreferences::setOverviewLayout,
            look = overviewCardLook,
            bar = bar,
            barLook = overviewActionLook,
            onBarChange = app.uiPreferences::setOverviewBar,
            modifier = Modifier.pageContent(padding),
            absent = absentCards,
        )
        else if (config?.activeSummary?.needsEndpoint == true) NoEndpointsBox(
            onScan = { scanningEndpoints = true },
            onAddEndpoint = { editingEndpoints = config?.activeContext }.takeIf { !clusterLabels.masked },
            modifier = Modifier.padding(padding),
        )
        else when (val s = state) {
            UiState.Loading -> LoadingBox(Modifier.pageContent(padding))
            is UiState.Failed -> ErrorBox(s.message, vm::refresh, Modifier.pageContent(padding))
            is UiState.Loaded -> PullToRefreshBox(
                isRefreshing = s.refreshing,
                onRefresh = {
                    // A node may have been upgraded since: ask again what each one supports.
                    vm.talos.forgetFeatures()
                    vm.refresh()
                    timeVm.refresh()
                    appsVm.refresh()
                    if (dataHints.isNotEmpty()) dataVm.refresh()
                    scope.launch { discoveryVm.discover() }
                },
                modifier = Modifier.pageContent(padding).fillMaxSize(),
            ) {
                val down = s.data.outage
                // With nodes known from before, they stay listed under a banner instead.
                if (down != null && !showNodesAnyway && !s.data.hasLastKnown) ClusterUnreachableBox(
                    outage = down,
                    endpoints = config?.activeSummary?.endpoints.orEmpty(),
                    onRetry = vm::refresh,
                    onShowNodes = { showNodesAnyway = true },
                    onScan = { scanningEndpoints = true }.takeIf { config?.activeSummary?.demo == false },
                    onEditEndpoints = { editingEndpoints = config?.activeContext }
                        .takeIf { config?.activeSummary?.demo == false && !clusterLabels.masked },
                ) else NodeList(
                    overview = s.data,
                    // In the map's order, site by site, once the KubeSpan map was fetched (or kept from before).
                    topology = remember(s.data) { vm.talos.cached<ClusterTopology>(TOPOLOGY)?.value },
                    outage = down?.takeIf { s.data.hasLastKnown },
                    onRetry = vm::refresh,
                    clusterName = config?.activeSummary?.let(clusterLabels::of),
                    fingerprint = config?.activeSummary?.fingerprint,
                    time = timeState,
                    certificate = config?.activeSummary,
                    onIssueConfig = onIssueConfig,
                    onInsights = onInsights,
                    apps = apps,
                    onApps = onApps,
                    dataServices = dataServices.takeIf { dataHints.isNotEmpty() },
                    dataHints = dataHints,
                    onDataServices = onDataServices,
                    argo = argo.takeIf { argoHinted },
                    onArgoCD = onArgoCD,
                    flux = flux.takeIf { fluxHinted },
                    onFlux = onFlux,
                    onNode = onNode,
                    onSettings = onSettings,
                    onFunding = onFunding,
                    onNodeAction = onNodeAction,
                    canPower = config?.activeSummary?.allows(Feature.POWER) == true,
                    canShell = config?.activeSummary?.allows(Feature.DEBUG_SHELL) == true,
                    canDrain = config?.activeSummary?.allows(Feature.WORKLOADS) == true,
                    canUpgrade = config?.activeSummary?.allows(Feature.UPGRADE) == true,
                    onUpgrade = onUpgrade,
                    skippedTalosUpdate = config?.activeSummary?.fingerprint?.let(skippedTalosUpdates::get),
                    onSkipTalosUpdate = skipTalosUpdate,
                    live = liveState.takeIf { liveEnabled },
                    discovered = discovered.size,
                    onDiscovered = { showDiscovered = true },
                    canDetectPublicIps = config?.activeSummary?.allows(Feature.KUBECONFIG) == true,
                    layout = layout,
                    onCustomize = customize,
                    nodesExpanded = nodesExpanded,
                    onToggleNodes = { app.uiPreferences.setNodesExpanded(!nodesExpanded) },
                    onChangelog = onChangelog,
                    onAllNodes = onAllNodes,
                )
            }
        }
    }
}

@Composable
private fun NodeList(
    overview: ClusterOverview,
    topology: ClusterTopology?,
    outage: ClusterOutage?,
    onRetry: () -> Unit,
    clusterName: String?,
    fingerprint: String?,
    time: UiState<ClusterTime>,
    certificate: ContextSummary?,
    onIssueConfig: () -> Unit,
    onInsights: () -> Unit,
    apps: UiState<Inventory>,
    onApps: (attention: Boolean) -> Unit,
    dataServices: UiState<DataServices>?,
    dataHints: String,
    onDataServices: (DataServiceKind?) -> Unit,
    argo: UiState<ArgoStatus>?,
    onArgoCD: () -> Unit,
    flux: UiState<FluxStatus>?,
    onFlux: () -> Unit,
    onNode: (NodeOverview) -> Unit,
    onSettings: () -> Unit,
    onFunding: () -> Unit,
    onNodeAction: (NodeOverview, NodeAction) -> Unit,
    canPower: Boolean,
    canShell: Boolean,
    canDrain: Boolean,
    canUpgrade: Boolean,
    onUpgrade: (NodeOverview, String) -> Unit,
    skippedTalosUpdate: String?,
    onSkipTalosUpdate: (String) -> Unit,
    live: ClusterLiveState?,
    discovered: Int,
    onDiscovered: () -> Unit,
    canDetectPublicIps: Boolean,
    layout: OverviewLayout,
    onCustomize: () -> Unit,
    nodesExpanded: Boolean,
    onToggleNodes: () -> Unit,
    onChangelog: () -> Unit,
    onAllNodes: (NodeFilter?) -> Unit,
) {
    var sheetFor by remember { mutableStateOf<NodeOverview?>(null) }
    val wakeOnLan = rememberWakeOnLan(fingerprint)
    val publicIps = rememberPublicIpDetection(fingerprint, canDetectPublicIps && overview.lacksPublicIps())
    RecordNodeMacs(fingerprint, overview.nodes)
    sheetFor?.let { node ->
        NodeActionsSheet(
            node = node,
            canPower = canPower,
            canShell = canShell,
            wol = wakeOnLan(node),
            canDrain = canDrain,
            onAction = { onNodeAction(node, it) },
            onDismiss = { sheetFor = null },
        )
    }
    val nodeGroups = remember(overview, topology) { topology.groupNodes(overview.nodes) }
    // Not asked (an internet call) for the demo cluster, or while its card is hidden.
    val talosCheck = if (certificate?.isDemo == true || OverviewCard.TALOS_UPDATE in layout.hidden) null
    else rememberTalosUpdateCheck(overview.nodes)
    val talosUpdate = remember(talosCheck, overview.nodes, skippedTalosUpdate) {
        talosCheck?.let { talosUpdateOffer(it, overview.nodes, skippedTalosUpdate) }
    }
    // The rollout stays open across the upgrade screen and until closed, even once its card is
    // gone (no reachable node is outdated any more).
    var rollingOut by rememberSaveable { mutableStateOf(false) }
    if (rollingOut && canUpgrade && talosCheck != null) {
        TalosRolloutDialog(overview.nodes, talosCheck.latest, onUpgrade = { onUpgrade(it, talosCheck.latest) }, onDismiss = { rollingOut = false })
    }
    LazyColumn(
        contentPadding = PaddingValues(16.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
        modifier = Modifier.fillMaxSize(),
    ) {
        outage?.let { item { LastKnownBanner(it, onRetry) } }
        if (certificate?.isDemo == true) item {
            Card(Modifier.fillMaxWidth()) {
                Text(stringResource(R.string.demo_notice), modifier = Modifier.padding(16.dp), style = MaterialTheme.typography.bodyMedium)
            }
        }
        if (BuildConfig.SELF_UPDATE) item { UpdateBanner(onClick = onSettings) } else item { StoreUpdateBanner() }
        if (BuildConfig.DONATIONS || BuildConfig.FEATURE_FUNDING) item { SupportCard(onFunding) }
        certificate?.let { item { CertificateBanner(it, onIssueConfig) } }
        if (discovered > 0) item { DiscoveredNodesBanner(discovered, onDiscovered) }
        // The cards, as arranged; a long press on one opens the arrangement.
        layout.visible.forEach { card ->
            when (card) {
                OverviewCard.TALOS_UPDATE -> if (talosUpdate != null) item(key = card.name) {
                    Box(Modifier.longPressToCustomize(onCustomize)) {
                        TalosUpdateCard(
                            talosUpdate,
                            canUpgrade,
                            onRollout = { rollingOut = true },
                            onSkip = { onSkipTalosUpdate(talosUpdate.latest) },
                        )
                    }
                }
                OverviewCard.SUMMARY -> item(key = card.name) {
                    ClusterSummaryCard(
                        clusterName ?: overview.context,
                        clusterSummary(overview.nodes),
                        live,
                        onInsights,
                        Modifier.longPressToCustomize(onCustomize),
                    )
                }
                OverviewCard.APPS -> item(key = card.name) {
                    val argoData = (argo as? UiState.Loaded)?.data
                    val inventory = (apps as? UiState.Loaded)?.data
                    val badges = remember(argoData, inventory) { if (argoData != null && inventory != null) argoData.inventoryBadges(inventory.apps) else emptyMap() }
                    Box(Modifier.longPressToCustomize(onCustomize)) { AppsCard(apps, onOpen = { onApps(false) }, argoBadges = badges, onAttention = { onApps(true) }) }
                }
                OverviewCard.DATA_SERVICES -> if (dataServices != null) item(key = card.name) {
                    val inventory = (apps as? UiState.Loaded)?.data
                    val appsById = remember(inventory) { inventory?.apps.orEmpty().associateBy { it.id } }
                    val hinted = remember(dataHints) { DataServiceKind.entries.filter { it.catalogId in dataHints.split(',') } }
                    val downNodes = remember(overview) { overview.downHostnames() }
                    Box(Modifier.longPressToCustomize(onCustomize)) { DataServicesCard(dataServices, hinted, appsById, downNodes, onDataServices) }
                }
                OverviewCard.ARGO_CD -> if (argo != null) item(key = card.name) {
                    val argoTile = (apps as? UiState.Loaded)?.data?.apps?.firstOrNull { it.id == ARGO_CD_CATALOG_ID }
                    val downNodes = remember(overview) { overview.downHostnames() }
                    Box(Modifier.longPressToCustomize(onCustomize)) { ArgoCard(argo, argoTile, downNodes, onArgoCD) }
                }
                OverviewCard.FLUX -> if (flux != null) item(key = card.name) {
                    val fluxTile = (apps as? UiState.Loaded)?.data?.apps?.firstOrNull { it.id == FLUX_CATALOG_ID }
                    Box(Modifier.longPressToCustomize(onCustomize)) { FluxCard(flux, fluxTile, onFlux) }
                }
                OverviewCard.NODES -> if (overview.nodes.isNotEmpty()) item(key = card.name) {
                    NodesCard(
                        nodeGroups,
                        publicIps,
                        expanded = nodesExpanded,
                        onToggle = onToggleNodes,
                        onNode = onNode,
                        onLive = { onNodeAction(it, NodeAction.LIVE) },
                        onMore = { sheetFor = it },
                        onAllNodes = onAllNodes,
                        // Only on the title: a long press on a node row opens its actions.
                        titleModifier = Modifier.longPressToCustomize(onCustomize),
                    )
                }
                OverviewCard.TIME_DRIFT -> item(key = card.name) {
                    Box(Modifier.longPressToCustomize(onCustomize)) {
                        TimeDriftCard(time, overview.nodes.associate { it.node to it.hostname })
                    }
                }
            }
        }
        // The Talos update card alone, with no release to offer, leaves the overview as empty.
        if (layout.visible.none { it != OverviewCard.TALOS_UPDATE || talosUpdate != null }) item(key = "all-hidden") {
            Card(onClick = onCustomize, modifier = Modifier.fillMaxWidth()) {
                MutedText(stringResource(R.string.overview_edit_all_hidden), modifier = Modifier.padding(16.dp))
            }
        }
        item(key = "version") { VersionFooter(onClick = onChangelog) }
    }
}

/**
 * Shown when the client certificate expires within [CERT_WARN_DAYS] days (like the alert).
 * An os:admin can renew it right away; anyone else needs a new talosconfig from an admin.
 */
@Composable
private fun CertificateBanner(summary: ContextSummary, onIssueConfig: () -> Unit) {
    if (summary.certNotAfter <= 0) return
    val days = daysUntil(summary.certNotAfter)
    if (days > CERT_WARN_DAYS) return
    val canRenew = summary.allows(Feature.ISSUE_CONFIG)
    val count = kotlin.math.abs(days).toInt()
    val text = if (days < 0) {
        pluralStringResource(R.plurals.overview_cert_expired, count, count)
    } else {
        pluralStringResource(R.plurals.overview_cert_expires, count, count)
    }
    val color = if (days < 0) LocalStatusColors.current.bad else LocalStatusColors.current.warn
    val content: @Composable () -> Unit = {
        Column(Modifier.padding(16.dp)) {
            Text(text, style = MaterialTheme.typography.bodyMedium, color = color)
            MutedText(stringResource(if (canRenew) R.string.overview_cert_renew else R.string.overview_cert_ask_admin))
        }
    }
    if (canRenew) Card(onClick = onIssueConfig, modifier = Modifier.fillMaxWidth()) { content() }
    else Card(Modifier.fillMaxWidth()) { content() }
}

/**
 * Reminds that screenshot mode is on, i.e. names and addresses on screen are not the real
 * ones. A small icon rather than a label, so it stays out of the way in the screenshots.
 */
@Composable
internal fun ScreenshotModeIcon() {
    val prefs = (LocalContext.current.applicationContext as TalosApp).uiPreferences
    val mask by prefs.privacyMask.collectAsStateWithLifecycle()
    if (!mask.enabled) return
    Icon(
        Icons.Outlined.VisibilityOff,
        contentDescription = stringResource(R.string.settings_screenshot_mode),
        tint = MaterialTheme.colorScheme.primary,
        modifier = Modifier.padding(end = 4.dp).size(14.dp),
    )
}

/** Shown when the daily check found a newer release; opens Settings → Updates. */
@Composable
internal fun UpdateBanner(onClick: () -> Unit) {
    val updates = (LocalContext.current.applicationContext as TalosApp).updateManager
    val state by updates.state.collectAsStateWithLifecycle()
    val available = state as? UpdateState.Available ?: return
    Card(onClick = onClick, modifier = Modifier.fillMaxWidth()) {
        Text(
            stringResource(R.string.overview_update_banner, available.info.version),
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.primary,
            modifier = Modifier.padding(16.dp),
        )
    }
}

/** Play build: an update Play has (declined at launch), being downloaded, or ready to install. */
@Composable
internal fun StoreUpdateBanner() {
    val updates = (LocalContext.current.applicationContext as TalosApp).storeUpdater
    val state by updates.state.collectAsStateWithLifecycle()
    val (text, onClick) = when (state) {
        StoreUpdateState.None -> return
        StoreUpdateState.Available -> R.string.overview_store_update_available to updates::start
        StoreUpdateState.Downloading -> R.string.overview_store_update_downloading to null
        StoreUpdateState.Downloaded -> R.string.overview_store_update_ready to updates::install
    }
    val content: @Composable () -> Unit = {
        Text(
            stringResource(text),
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.primary,
            modifier = Modifier.padding(16.dp),
        )
    }
    if (onClick != null) Card(onClick = onClick, modifier = Modifier.fillMaxWidth()) { content() }
    else Card(Modifier.fillMaxWidth()) { content() }
}

/**
 * Occasional, dismissable ask to support the project (see SupportPrompt for the timing): GitHub
 * Sponsors in the open-source builds, [onFunding] (Play in-app purchases) in the Play build.
 */
@Composable
internal fun SupportCard(onFunding: () -> Unit) {
    val context = LocalContext.current
    val prompt = (context.applicationContext as TalosApp).supportPrompt
    val visible by prompt.visible.collectAsStateWithLifecycle()
    if (!visible) return
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp)) {
            Text(stringResource(R.string.overview_support_title), style = MaterialTheme.typography.titleSmall)
            MutedText(stringResource(R.string.overview_support_body))
            Row(horizontalArrangement = Arrangement.End, modifier = Modifier.fillMaxWidth()) {
                TextButton(onClick = prompt::never) { Text(stringResource(R.string.overview_support_never)) }
                TextButton(onClick = prompt::later) { Text(stringResource(R.string.overview_support_later)) }
                TextButton(onClick = {
                    prompt.later()
                    if (BuildConfig.FEATURE_FUNDING) onFunding() else openUrl(context, SPONSOR_URL)
                }) { Text(stringResource(if (BuildConfig.FEATURE_FUNDING) R.string.funding_title else R.string.overview_support_sponsor)) }
            }
        }
    }
}
