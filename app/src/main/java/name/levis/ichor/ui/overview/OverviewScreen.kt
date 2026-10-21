package name.levis.ichor.ui.overview

import name.levis.ichor.ui.components.rememberClusterLabels
import androidx.activity.compose.BackHandler
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.res.stringResource
import name.levis.ichor.R
import name.levis.ichor.monitor.freezeReminderHook
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.ui.unit.dp
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.AutoAwesome
import androidx.compose.material3.Icon
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SmallFloatingActionButton
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.SideEffect
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.flow.first
import name.levis.ichor.util.StartupTrace
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Modifier
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import androidx.compose.ui.platform.LocalContext
import name.levis.ichor.TalosApp
import name.levis.ichor.data.ConfigRepository
import name.levis.ichor.data.activeSummary
import name.levis.ichor.model.isDemo
import name.levis.ichor.model.needsEndpoint
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.data.activeIsKube
import name.levis.ichor.data.TOPOLOGY
import name.levis.ichor.model.ClusterTopology
import name.levis.ichor.data.OVERVIEW
import name.levis.ichor.model.ClusterOverview
import name.levis.ichor.model.outage
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.DataFreshness
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.factory
import kotlinx.coroutines.launch
import name.levis.ichor.ui.apps.AppsViewModel
import name.levis.ichor.ui.plugins.PluginDetectViewModel
import name.levis.ichor.ui.flux.FluxViewModel
import name.levis.ichor.ui.argocd.ArgoViewModel
import name.levis.ichor.ui.dataservices.DataServicesViewModel
import name.levis.ichor.ui.components.pageContent
import name.levis.ichor.ui.components.ReadableWidth
import name.levis.ichor.ui.components.WideReadableWidthDp
import name.levis.ichor.ui.components.readableWidth
import name.levis.ichor.ui.components.rememberTwoPanes
import name.levis.ichor.ui.tour.DemoTourHost

class OverviewViewModel(
    val talos: TalosRepository,
    val configs: ConfigRepository,
    /** The last background check's view of the cluster, when nothing is cached (see [LoadingViewModel.seed]). */
    private val monitorSeed: () -> TalosRepository.Timed<ClusterOverview>? = { null },
) : LoadingViewModel<ClusterOverview>() {
    override val keepsDataOnFailure = true
    override fun cached(): TalosRepository.Timed<ClusterOverview>? =
        talos.cached<ClusterOverview>(OVERVIEW).also { StartupTrace.markOnce(if (it == null) "overview cached miss" else "overview cached hit") }
    override fun seed(): TalosRepository.Timed<ClusterOverview>? =
        monitorSeed().also { StartupTrace.markOnce(if (it == null) "overview seed miss" else "overview seed hit") }
    override val restores get() = talos.restores
    override suspend fun fetch(): ClusterOverview {
        StartupTrace.markOnce("overview fetch start")
        // Each node as it answers while nothing is on screen; a refresh keeps the old rows (L12).
        return talos.overview(onPartial = ::showPartial)
    }

    init {
        // The first data on the home, the last known one or the fresh one.
        viewModelScope.launch {
            val loaded = state.first { it is UiState.Loaded } as UiState.Loaded<*>
            val kind = when {
                (loaded.data as? ClusterOverview)?.partial == true -> "(partial)"
                loaded.refreshing -> "(last known)"
                else -> "(fresh)"
            }
            StartupTrace.markOnce("overview first Loaded", kind)
        }
    }
}


@Composable
fun OverviewScreen(
    nav: OverviewScreenNavigation,
    vm: OverviewViewModel = viewModel(factory = factory { OverviewViewModel(app.talosRepository, app.configRepository, app::overviewSeed) }),
    timeVm: ClusterTimeViewModel = viewModel(factory = factory { ClusterTimeViewModel(app.talosRepository) }),
    liveVm: ClusterLiveViewModel = viewModel(factory = factory { ClusterLiveViewModel(app.talosRepository) }),
    discoveryVm: NodeDiscoveryViewModel = viewModel(factory = factory { NodeDiscoveryViewModel(app.nodeLifecycleRepository) }),
    appsVm: AppsViewModel = viewModel(key = "overview-apps", factory = factory { AppsViewModel(app.talosRepository) }),
    dataVm: DataServicesViewModel = viewModel(key = "overview-data-services", factory = factory { DataServicesViewModel(app.dataServicesRepository) }),
    argoVm: ArgoViewModel = viewModel(key = "overview-argocd", factory = factory { ArgoViewModel(app.gitOpsRepository, app.kubeRepository, freezeReminderHook(app)) }),
    fluxVm: FluxViewModel = viewModel(key = "overview-flux", factory = factory { FluxViewModel(app.gitOpsRepository, app.kubeRepository) }),
    alertsVm: AlertsCardViewModel = viewModel(key = "overview-alerts", factory = factory { alertsCardViewModel(app) }),
    pluginsVm: PluginDetectViewModel = viewModel(key = "overview-plugins", factory = factory { PluginDetectViewModel(app.pluginRepository) }),
) {
    SideEffect { StartupTrace.markOnce("overviewScreen first composition") }
    // A cluster added from a kubeconfig has no Talos overview: its Kubernetes home instead,
    // before any of the Talos loads below start.
    val shown by vm.configs.config.collectAsStateWithLifecycle()
    if (shown?.activeIsKube == true) {
        KubeHomeScreen(nav.kubeHome())
        return
    }
    TalosOverview(nav, vm, timeVm, liveVm, discoveryVm, OverviewCardViewModels(appsVm, dataVm, argoVm, fluxVm, alertsVm, pluginsVm))
}

/** The Talos cluster's overview: its nodes, the cards around them, the bar and its sheets. */
@Composable
private fun TalosOverview(
    nav: OverviewScreenNavigation,
    vm: OverviewViewModel,
    timeVm: ClusterTimeViewModel,
    liveVm: ClusterLiveViewModel,
    discoveryVm: NodeDiscoveryViewModel,
    cardVms: OverviewCardViewModels,
) {
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
    val cards = rememberOverviewCards(app, vm.configs, config, invalidations, cardVms)

    // No node answered (VPN off, another network): one notice instead of a list of red nodes.
    val outage = (state as? UiState.Loaded)?.data?.outage
    var showNodesAnyway by remember(config?.activeContext) { mutableStateOf(false) }
    OverviewRetryEffects(vm, app, outage, config?.activeContext, invalidations, onOutage = { showNodesAnyway = false })

    val liveEnabled by app.uiPreferences.liveClusterStats.collectAsStateWithLifecycle()
    val liveState by liveVm.state.collectAsStateWithLifecycle()
    // Live stats and discovery only make sense once a node answers (not on the background check's seed).
    // Not on a partial overview either: its node count still grows.
    val loaded = (state as? UiState.Loaded)?.let { !it.fromMonitor && !it.data.partial } == true && outage == null
    LiveStatsEffects(app, liveVm, state, liveEnabled, loaded, config?.activeContext, invalidations)

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
    val skipTalosUpdate = skipTalosUpdateAction(app, context, config, skippedTalosUpdates, scope, snackbar)

    // The demo tour, once, on the Talos demo home; not over the editor, a menu or a screenshot-mode switch.
    val tourPending by app.uiPreferences.tourPending.collectAsStateWithLifecycle()
    val maskSwitching by app.maskTransition.collectAsStateWithLifecycle()
    val touring = tourPending && config?.activeSummary?.isDemo == true && state is UiState.Loaded &&
        !customizing && !clusterMenu && !showClusters && !maskSwitching
    DemoTourHost(active = touring, onFinish = { app.uiPreferences.setTourPending(false) }) {
        Scaffold(
            bottomBar = { DataFreshness(state) },
            snackbarHost = { SnackbarHost(snackbar) },
            // The AI diagnosis is optional: no trace of it unless it was turned on in Settings.
            floatingActionButton = {
                if (ai.enabled && !customizing) {
                    SmallFloatingActionButton(onClick = nav.onDiagnose) {
                        Icon(Icons.Outlined.AutoAwesome, stringResource(R.string.ai_title))
                    }
                }
            },
            topBar = {
                OverviewTopBar(
                    app, nav, config, clusterColors, clusterLabels,
                    customizing = customizing,
                    onCustomizing = { customizing = it },
                    clusterMenu = clusterMenu,
                    onClusterMenu = { clusterMenu = it },
                    onManageClusters = { showClusters = true },
                    bar = bar,
                    state = state,
                    cards = cards,
                )
            },
        ) { padding ->
            OverviewDialogs(
                app, nav, config, clusterColors, clusterLabels,
                showClusters = showClusters,
                onCloseClusters = { showClusters = false },
                endpoints = EndpointEditing(editingEndpoints, scanningEndpoints, { editingEndpoints = it }, { scanningEndpoints = it }),
                discovery = discoveryVm,
                discovered = discovered,
                showDiscovered = showDiscovered,
                onCloseDiscovered = { showDiscovered = false },
                scope = scope,
                context = context,
            )
            if (customizing) OverviewEditor(app, layout, bar, cards.absent, Modifier.pageContent(padding))
            else if (config?.activeSummary?.needsEndpoint == true) NoEndpointsBox(
                onScan = { scanningEndpoints = true },
                onAddEndpoint = { editingEndpoints = config?.activeContext }.takeIf { !clusterLabels.masked },
                modifier = Modifier.padding(padding),
            )
            else when (val s = state) {
                UiState.Loading -> LoadingBox(Modifier.pageContent(padding))
                is UiState.Failed -> ErrorBox(s.message, vm::refresh, Modifier.pageContent(padding))
                is UiState.Loaded -> OverviewLoaded(
                    s,
                    // Two columns on a tablet need the wider column (OverviewItems).
                    Modifier.padding(padding).readableWidth(if (rememberTwoPanes()) WideReadableWidthDp.dp else ReadableWidth).fillMaxSize(),
                    onRefresh = { refreshOverview(vm, timeVm, discoveryVm, cardVms, cards, scope) },
                    onRetry = vm::refresh,
                    topology = { vm.talos.cached<ClusterTopology>(TOPOLOGY)?.value },
                    summary = config?.activeSummary,
                    clusterLabels = clusterLabels,
                    unreachable = UnreachableActions(
                        showNodes = showNodesAnyway,
                        onShowNodes = { showNodesAnyway = true },
                        onScan = { scanningEndpoints = true },
                        onEditEndpoints = { editingEndpoints = config?.activeContext },
                    ),
                    list = OverviewListState(
                        time = timeState,
                        cards = cards.data,
                        skippedTalosUpdate = config?.activeSummary?.fingerprint?.let(skippedTalosUpdates::get),
                        onSkipTalosUpdate = skipTalosUpdate,
                        live = liveState.takeIf { liveEnabled },
                        discovered = discovered.size,
                        onDiscovered = { showDiscovered = true },
                        layout = layout,
                        onCustomize = customize,
                        nodesExpanded = nodesExpanded,
                        onToggleNodes = { app.uiPreferences.setNodesExpanded(!nodesExpanded) },
                    ),
                    nav = nav,
                )
            }
        }
    }
}
