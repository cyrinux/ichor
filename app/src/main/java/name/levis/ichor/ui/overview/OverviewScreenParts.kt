package name.levis.ichor.ui.overview

import androidx.compose.ui.platform.testTag
import name.levis.ichor.ui.components.SMOKE_CLUSTER_TITLE
import name.levis.ichor.selectCluster
import name.levis.ichor.addClusterNodes
import android.content.Context
import android.widget.Toast
import androidx.compose.foundation.layout.Box
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.SnackbarDuration
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.SnackbarResult
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.produceState
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.repeatOnLifecycle
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.ConfigRepository
import name.levis.ichor.data.LiveFocus
import name.levis.ichor.data.StoredConfig
import name.levis.ichor.data.activeSummary
import name.levis.ichor.model.ClusterOutage
import name.levis.ichor.model.ClusterOverview
import name.levis.ichor.model.DiscoveredNode
import name.levis.ichor.model.Feature
import name.levis.ichor.model.OverviewBar
import name.levis.ichor.model.OverviewCard
import name.levis.ichor.model.PluginCatalog
import name.levis.ichor.model.allows
import name.levis.ichor.model.dataServiceHints
import name.levis.ichor.model.hasArgoCD
import name.levis.ichor.model.hasFlux
import name.levis.ichor.model.needsAttention
import name.levis.ichor.model.outage
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.apps.AppsViewModel
import name.levis.ichor.ui.argocd.ArgoViewModel
import name.levis.ichor.ui.cancellableCatching
import name.levis.ichor.model.ClusterLabels
import name.levis.ichor.ui.dataservices.DataServicesViewModel
import name.levis.ichor.ui.flux.FluxViewModel
import name.levis.ichor.ui.gitops.hasClusterRepo
import name.levis.ichor.ui.plugins.PluginDetectViewModel
import name.levis.ichor.ui.share.rememberClusterPins
import name.levis.ichor.ui.tour.TourAnchor
import name.levis.ichor.ui.tour.tourAnchor
import name.levis.ichor.ui.userMessage
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.remember
import name.levis.ichor.model.ClusterTime
import name.levis.ichor.model.ClusterTopology
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.OverviewLayout
import name.levis.ichor.model.hasLastKnown
import kotlin.time.Duration.Companion.seconds

/** The ViewModels of the overview's cards besides the nodes. */
internal class OverviewCardViewModels(
    val apps: AppsViewModel,
    val dataServices: DataServicesViewModel,
    val argo: ArgoViewModel,
    val flux: FluxViewModel,
    val alerts: AlertsCardViewModel,
    val plugins: PluginDetectViewModel,
)

/** The overview cards' data and what decided it (see [rememberOverviewCards]). */
internal class OverviewCardsState(
    val data: OverviewCardData,
    val argoHinted: Boolean,
    val fluxHinted: Boolean,
    val alertsAllowed: Boolean,
    /** Cards the cluster has nothing for, left out of the editor too; all offered until the inventory is known. */
    val absent: Set<OverviewCard>,
)

/**
 * Loads what the overview's cards show besides the nodes, each only when the inventory hints
 * the cluster has it and the role may read it; keyed on the cluster, its config generation and
 * [invalidations] like the overview itself.
 */
@Composable
internal fun rememberOverviewCards(
    app: TalosApp,
    configs: ConfigRepository,
    config: StoredConfig?,
    invalidations: Int,
    vms: OverviewCardViewModels,
): OverviewCardsState {
    val appsVm = vms.apps
    val dataVm = vms.dataServices
    val pluginsVm = vms.plugins
    val argoVm = vms.argo
    val fluxVm = vms.flux
    val alertsVm = vms.alerts
    // Once per cluster and config generation (adding nodes or new credentials keep the context):
    // listing every node's containers is too heavy to repeat on each visit.
    val generation by configs.generation.collectAsStateWithLifecycle()
    LaunchedEffect(config?.activeContext, generation, invalidations) {
        appsVm.load(Triple(config?.activeContext, generation, invalidations))
    }
    val apps by appsVm.state.collectAsStateWithLifecycle()

    // The installed plugins (no cluster call, kept once read): those of Data services hint too.
    val pluginCatalog by produceState(PluginCatalog(), config?.activeContext, invalidations) {
        value = cancellableCatching { app.pluginRepository.catalog() }.getOrDefault(PluginCatalog())
    }
    // Longhorn, Garage, CloudNativePG, a Data services plugin (Rook...): only asked (through Kubernetes)
    // when the inventory shows one of them and the role may use the Kubernetes API; other clusters pay nothing.
    val dataHints = (apps as? UiState.Loaded)?.data?.dataServiceHints(pluginCatalog).orEmpty()
        .takeIf { config?.activeSummary?.allows(Feature.WORKLOADS) == true }.orEmpty()
    LaunchedEffect(config?.activeContext, generation, invalidations, dataHints) {
        // Another cluster without hints: forgotten, so coming back loads again instead of showing the old result.
        if (dataHints.isNotEmpty()) dataVm.load(listOf(config?.activeContext, generation, invalidations, dataHints), dataHints) else dataVm.forget()
        // The CRD plugins (kube-green...) with them: their tiles join the card.
        if (dataHints.isNotEmpty()) pluginsVm.load(listOf(config?.activeContext, generation, invalidations), dataHints) else pluginsVm.forget()
    }
    val dataServices by dataVm.state.collectAsStateWithLifecycle()
    val plugins by pluginsVm.state.collectAsStateWithLifecycle()
    val dataPlugins = (plugins as? UiState.Loaded)?.data?.dataServicePlugins.orEmpty()
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
    // The Alertmanager: no inventory hint, it is looked for among the Services (a quick list).
    // The Kubernetes screens card too: both need the role to use the Kubernetes API.
    val kubernetes = config?.activeSummary?.allows(Feature.WORKLOADS) == true
    val alertsAllowed = kubernetes
    LaunchedEffect(config?.activeContext, generation, invalidations, alertsAllowed) {
        if (alertsAllowed) alertsVm.load(listOf(config?.activeContext, generation, invalidations)) else alertsVm.forget()
    }
    val alerts by alertsVm.state.collectAsStateWithLifecycle()
    val alertsShown = alerts.takeIf { alertsAllowed && it.alertmanagerFound }
    // The operators behind the Kubernetes screens card's conditional screens: only while it shows.
    val layout by app.uiPreferences.overviewLayout.collectAsStateWithLifecycle()
    val operators = rememberKubeOperators(
        active = kubernetes && OverviewCard.TOOLS in layout.visible,
        key = listOf(config?.activeContext, generation, invalidations),
    )
    // The objects pinned on this cluster (kept apart while screenshot mode is on).
    val pins = rememberClusterPins()
    val absentCards = setOfNotNull(
        OverviewCard.PINNED.takeIf { pins?.pins.isNullOrEmpty() },
        OverviewCard.TOOLS.takeIf { !kubernetes },
    ) + if (apps is UiState.Loaded) setOfNotNull(
        OverviewCard.DATA_SERVICES.takeIf { dataHints.isEmpty() },
        OverviewCard.ARGO_CD.takeIf { !argoHinted },
        OverviewCard.FLUX.takeIf { !fluxHinted },
        OverviewCard.ALERTS.takeIf { alertsShown == null && (!alertsAllowed || alerts !is UiState.Loading) },
    ) else emptySet()
    return OverviewCardsState(
        OverviewCardData(
            apps = apps,
            dataServices = dataServices.takeIf { dataHints.isNotEmpty() },
            dataHints = dataHints,
            dataPlugins = dataPlugins,
            argo = argo.takeIf { argoHinted },
            flux = flux.takeIf { fluxHinted },
            alerts = alertsShown,
            pins = pins,
            kubeTools = kubernetes,
            toolPlugins = (plugins as? UiState.Loaded)?.data?.homeTiles.orEmpty(),
            operators = operators,
        ),
        argoHinted = argoHinted,
        fluxHinted = fluxHinted,
        alertsAllowed = alertsAllowed,
        absent = absentCards,
    )
}

/**
 * While no node answers ([outage]), the overview is retried every [UNREACHABLE_RETRY_SECONDS]
 * on screen ([onOutage] first); a new network (VPN connected, back on Wi-Fi) retries a failed
 * one at once.
 */
@Composable
internal fun OverviewRetryEffects(
    vm: OverviewViewModel,
    app: TalosApp,
    outage: ClusterOutage?,
    activeContext: String?,
    invalidations: Int,
    onOutage: () -> Unit,
) {
    val lifecycle = LocalLifecycleOwner.current.lifecycle
    LaunchedEffect(outage != null, activeContext, invalidations) {
        if (outage == null) return@LaunchedEffect
        onOutage()
        lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) {
            while (true) {
                delay(UNREACHABLE_RETRY_SECONDS.seconds)
                if ((vm.state.value as? UiState.Loaded)?.refreshing != true) vm.refresh()
            }
        }
    }
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
}

/**
 * Live CPU and memory only while the overview is on screen, once it [loaded], and if [enabled].
 * On a large cluster most samples ask only the focus set: the problem nodes of the last
 * overview, plus what the dense card and the Nodes screen show.
 */
@Composable
internal fun LiveStatsEffects(
    app: TalosApp,
    liveVm: ClusterLiveViewModel,
    state: UiState<ClusterOverview>,
    enabled: Boolean,
    loaded: Boolean,
    activeContext: String?,
    invalidations: Int,
) {
    val lifecycle = LocalLifecycleOwner.current.lifecycle
    // Fewer samples on a large cluster: each one asks every node.
    val nodeCount = (state as? UiState.Loaded)?.data?.nodes?.size ?: 0
    val problemNodes = (state as? UiState.Loaded)?.data?.nodes?.filter { it.needsAttention }?.map { it.node }?.toSet().orEmpty()
    LaunchedEffect(activeContext, invalidations, problemNodes) {
        app.liveFocus.scope(activeContext to invalidations)
        app.liveFocus.publish(LiveFocus.OVERVIEW, problemNodes)
    }
    LaunchedEffect(enabled, loaded, activeContext, invalidations, nodeCount) {
        if (!enabled) liveVm.clear()
        if (!enabled || !loaded) return@LaunchedEffect
        lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) {
            liveVm.poll(activeContext to invalidations, nodeCount, app.liveFocus.nodes)
        }
    }
}

/** Pull-to-refresh: the overview and every card it loaded, and what each node supports. */
internal fun refreshOverview(
    vm: OverviewViewModel,
    timeVm: ClusterTimeViewModel,
    discoveryVm: NodeDiscoveryViewModel,
    vms: OverviewCardViewModels,
    cards: OverviewCardsState,
    scope: CoroutineScope,
) {
    // A node may have been upgraded since: ask again what each one supports.
    vm.talos.forgetFeatures()
    vm.refresh()
    timeVm.refresh()
    vms.apps.refresh()
    if (cards.data.dataHints.isNotEmpty()) vms.dataServices.refresh()
    if (cards.data.dataHints.isNotEmpty()) vms.plugins.refresh()
    if (cards.alertsAllowed) vms.alerts.refresh()
    scope.launch { discoveryVm.discover() }
}

/** Skips the Talos release offered on this cluster's update card; the snackbar takes it back. */
internal fun skipTalosUpdateAction(
    app: TalosApp,
    context: Context,
    config: StoredConfig?,
    skipped: Map<String, String>,
    scope: CoroutineScope,
    snackbar: SnackbarHostState,
): (String) -> Unit = { version ->
    config?.activeSummary?.fingerprint?.let { fingerprint ->
        val before = skipped[fingerprint]
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

/** The overview's bar: the cluster title and menu with the actions, or Done while [customizing]. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun OverviewTopBar(
    app: TalosApp,
    nav: OverviewScreenNavigation,
    config: StoredConfig?,
    clusterColors: Map<String, Int>,
    clusterLabels: ClusterLabels,
    customizing: Boolean,
    onCustomizing: (Boolean) -> Unit,
    clusterMenu: Boolean,
    onClusterMenu: (Boolean) -> Unit,
    onManageClusters: () -> Unit,
    bar: OverviewBar,
    state: UiState<ClusterOverview>,
    cards: OverviewCardsState,
) {
    if (customizing) TopAppBar(
        title = { Text(stringResource(R.string.overview_edit_title)) },
        actions = { TextButton(onClick = { onCustomizing(false) }) { Text(stringResource(R.string.overview_edit_done)) } },
    ) else TopAppBar(
        // Swipe the bar sideways for the previous/next cluster, tap the title for the menu.
        modifier = Modifier.clusterSwipe(config, app::selectCluster),
        title = {
            Box(Modifier.tourAnchor(TourAnchor.Switcher).testTag(SMOKE_CLUSTER_TITLE)) {
                ClusterTitle(config, clusterColors, clusterLabels, onOpen = { onClusterMenu(true) }) { ScreenshotModeIcon() }
                config?.let { stored ->
                    ClusterMenu(
                        expanded = clusterMenu,
                        config = stored,
                        colors = clusterColors,
                        labels = clusterLabels,
                        onSelect = app::selectCluster,
                        onManage = onManageClusters,
                        onFleet = nav.onFleet,
                        onDismiss = { onClusterMenu(false) },
                        onUpgradeCluster = { nav.onUpgradeCluster("") },
                        onUpgradeKubernetes = nav.onUpgradeKubernetes,
                        onAddNode = nav.onAddNode,
                        onRunbooks = nav.onRunbooks,
                        onUpdates = nav.onUpdates,
                    )
                }
            }
        },
        actions = {
            OverviewActions(
                bar = bar,
                nav = OverviewNavigation(
                    onHealth = nav.onHealth,
                    onEvents = nav.onEvents,
                    onWorkloads = nav.onWorkloads,
                    onGitOps = if (cards.argoHinted) nav.onArgoCD else nav.onFlux,
                    onMetrics = nav.onMetrics,
                    onAlerts = nav.onAlerts,
                    onKubeSpan = nav.onKubeSpan,
                    onEtcd = nav.onEtcd,
                    onSettings = nav.onSettings,
                    onSearch = nav.onSearch,
                    onRepository = nav.onRepository,
                ),
                reachable = (state as? UiState.Loaded)?.data?.nodes?.filter { it.reachable }?.map { it.node },
                health = config?.activeSummary?.allows(Feature.HEALTH) == true,
                workloads = config?.activeSummary?.allows(Feature.WORKLOADS) == true,
                gitOps = cards.argoHinted || cards.fluxHinted,
                onCustomize = { onCustomizing(true) },
                repository = hasClusterRepo(),
            )
        },
    )
}

/** The overview's sheets and dialogs: manage clusters, their endpoints, and the members discovery found. */
@Composable
internal fun OverviewDialogs(
    app: TalosApp,
    nav: OverviewScreenNavigation,
    config: StoredConfig?,
    clusterColors: Map<String, Int>,
    clusterLabels: ClusterLabels,
    showClusters: Boolean,
    onCloseClusters: () -> Unit,
    endpoints: EndpointEditing,
    discovery: NodeDiscoveryViewModel,
    discovered: List<DiscoveredNode>,
    showDiscovered: Boolean,
    onCloseDiscovered: () -> Unit,
    scope: CoroutineScope,
    context: Context,
) {
    config?.takeIf { showClusters }?.let { stored ->
        ManageClustersSheet(
            config = stored,
            colors = clusterColors,
            labels = clusterLabels,
            onClose = onCloseClusters,
            onAddCluster = nav.onAddCluster,
            onClustersCleared = nav.onClustersCleared,
            onEndpoints = { endpoints.onEdit(it.name) },
            onActivity = { nav.onActivity(it.name) },
            onGitHosts = nav.onGitHosts,
            onFleet = nav.onFleet,
        )
    }
    config?.let { stored ->
        EndpointTools(
            config = stored,
            labels = clusterLabels,
            editing = endpoints.editing,
            scanning = endpoints.scanning,
            onEdit = endpoints.onEdit,
            onScan = endpoints.onScan,
        )
    }
    config?.activeContext?.takeIf { showDiscovered && discovered.isNotEmpty() }?.let { contextName ->
        AddDiscoveredNodesDialog(
            nodes = discovered,
            onAdd = { nodes ->
                onCloseDiscovered()
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
                onCloseDiscovered()
                discovery.dismiss(discovered)
            },
            onDismiss = onCloseDiscovered,
        )
    }
}

/** The cluster whose endpoints are being edited, and the network search for clusters' nodes. */
internal class EndpointEditing(
    val editing: String?,
    val scanning: Boolean,
    val onEdit: (String?) -> Unit,
    val onScan: (Boolean) -> Unit,
)

/** What the notice for a cluster no node answers offers (see [ClusterUnreachableBox]). */
internal class UnreachableActions(
    val showNodes: Boolean,
    val onShowNodes: () -> Unit,
    val onScan: () -> Unit,
    val onEditEndpoints: () -> Unit,
)

/** The screen's state [NodeList] shows besides the overview. */
internal class OverviewListState(
    val time: UiState<ClusterTime>,
    val cards: OverviewCardData,
    val skippedTalosUpdate: String?,
    val onSkipTalosUpdate: (String) -> Unit,
    val live: ClusterLiveState?,
    val discovered: Int,
    val onDiscovered: () -> Unit,
    val layout: OverviewLayout,
    val onCustomize: () -> Unit,
    val nodesExpanded: Boolean,
    val onToggleNodes: () -> Unit,
)

/**
 * The loaded overview under pull-to-refresh: its nodes and cards, or one notice when no node
 * answers and none is known from before.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun OverviewLoaded(
    s: UiState.Loaded<ClusterOverview>,
    modifier: Modifier,
    onRefresh: () -> Unit,
    onRetry: () -> Unit,
    topology: () -> ClusterTopology?,
    summary: ContextSummary?,
    clusterLabels: ClusterLabels,
    unreachable: UnreachableActions,
    list: OverviewListState,
    nav: OverviewScreenNavigation,
) {
    PullToRefreshBox(isRefreshing = s.refreshing, onRefresh = onRefresh, modifier = modifier) {
        val down = s.data.outage
        // With nodes known from before, they stay listed under a banner instead.
        if (down != null && !unreachable.showNodes && !s.data.hasLastKnown) ClusterUnreachableBox(
            outage = down,
            endpoints = summary?.endpoints.orEmpty(),
            onRetry = onRetry,
            onShowNodes = unreachable.onShowNodes,
            onScan = unreachable.onScan.takeIf { summary?.demo == false },
            onEditEndpoints = unreachable.onEditEndpoints.takeIf { summary?.demo == false && !clusterLabels.masked },
        ) else NodeList(
            overview = s.data,
            // In the map's order, site by site, once the KubeSpan map was fetched (or kept from before).
            topology = remember(s.data) { topology() },
            outage = down?.takeIf { s.data.hasLastKnown },
            onRetry = onRetry,
            clusterName = summary?.let(clusterLabels::of),
            fingerprint = summary?.fingerprint,
            time = list.time,
            certificate = summary,
            cards = list.cards,
            nav = nav,
            access = NodeListAccess(
                canPower = summary?.allows(Feature.POWER) == true,
                canShell = summary?.allows(Feature.DEBUG_SHELL) == true,
                canDrain = summary?.allows(Feature.WORKLOADS) == true,
                canUpgrade = summary?.allows(Feature.UPGRADE) == true,
                canDetectPublicIps = summary?.allows(Feature.KUBECONFIG) == true,
            ),
            skippedTalosUpdate = list.skippedTalosUpdate,
            onSkipTalosUpdate = list.onSkipTalosUpdate,
            live = list.live,
            discovered = list.discovered,
            onDiscovered = list.onDiscovered,
            layout = list.layout,
            onCustomize = list.onCustomize,
            nodesExpanded = list.nodesExpanded,
            onToggleNodes = list.onToggleNodes,
        )
    }
}

/** The arrangement of the home: its cards and its bar, with [absent] ones (nothing to show) left out. */
@Composable
internal fun OverviewEditor(app: TalosApp, layout: OverviewLayout, bar: OverviewBar, absent: Set<OverviewCard>, modifier: Modifier) {
    HomeEditor(
        layout = layout,
        onChange = app.uiPreferences::setOverviewLayout,
        look = overviewCardLook,
        bar = bar,
        barLook = overviewActionLook,
        onBarChange = app.uiPreferences::setOverviewBar,
        modifier = modifier,
        absent = absent,
    )
}
