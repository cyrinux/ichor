package name.levis.ichor.ui.overview

import name.levis.ichor.BuildConfig
import name.levis.ichor.ui.components.rememberClusterLabels
import android.widget.Toast
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import name.levis.ichor.R
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.AutoAwesome
import androidx.compose.material.icons.outlined.Favorite
import androidx.compose.material.icons.outlined.Hub
import androidx.compose.material.icons.outlined.Settings
import androidx.compose.material.icons.outlined.Storage
import androidx.compose.material.icons.outlined.Timeline
import androidx.compose.material.icons.outlined.VisibilityOff
import androidx.compose.material.icons.outlined.Widgets
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SmallFloatingActionButton
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
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontFamily
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
import name.levis.ichor.model.TalosFeature
import name.levis.ichor.model.clusterSupport
import name.levis.ichor.ui.components.rememberClusterFeatures
import name.levis.ichor.update.UpdateState
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.data.OVERVIEW
import name.levis.ichor.model.ClusterOverview
import name.levis.ichor.model.ClusterTime
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.monitor.CERT_WARN_DAYS
import name.levis.ichor.util.daysUntil
import name.levis.ichor.model.NodeOverview
import name.levis.ichor.model.clusterSummary
import name.levis.ichor.model.outage
import name.levis.ichor.model.ClusterOutage
import name.levis.ichor.model.hasLastKnown
import name.levis.ichor.ui.components.agoLabel
import name.levis.ichor.model.health
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.DataFreshness
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.NodeHealthPill
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
import name.levis.ichor.ui.dataservices.DataServicesViewModel
import name.levis.ichor.ui.dataservices.downHostnames
import name.levis.ichor.ui.components.TooltipIconButton

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
    onDataServices: () -> Unit,
    onHealth: () -> Unit,
    onEvents: () -> Unit,
    onInsights: () -> Unit,
    onApps: () -> Unit,
    onSettings: () -> Unit,
    onIssueConfig: () -> Unit,
    onUpgrade: (NodeOverview, String) -> Unit,
    onDiagnose: () -> Unit,
    onAddCluster: () -> Unit,
    onClustersCleared: () -> Unit,
    vm: OverviewViewModel = viewModel(factory = factory { OverviewViewModel(app.talosRepository, app.configRepository) }),
    timeVm: ClusterTimeViewModel = viewModel(factory = factory { ClusterTimeViewModel(app.talosRepository) }),
    liveVm: ClusterLiveViewModel = viewModel(factory = factory { ClusterLiveViewModel(app.talosRepository) }),
    discoveryVm: NodeDiscoveryViewModel = viewModel(factory = factory { NodeDiscoveryViewModel(app.talosRepository) }),
    appsVm: AppsViewModel = viewModel(key = "overview-apps", factory = factory { AppsViewModel(app.talosRepository) }),
    dataVm: DataServicesViewModel = viewModel(key = "overview-data-services", factory = factory { DataServicesViewModel(app.talosRepository) }),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    val context = LocalContext.current
    val app = context.applicationContext as TalosApp
    val ai by app.aiPreferences.settings.collectAsStateWithLifecycle()
    val clusterColors by app.clusterColors.colors.collectAsStateWithLifecycle()
    val vpnOnly by app.vpnOnly.fingerprints.collectAsStateWithLifecycle()
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
    LaunchedEffect(liveEnabled, loaded, config?.activeContext, invalidations) {
        if (!liveEnabled) liveVm.clear()
        if (!liveEnabled || !loaded) return@LaunchedEffect
        lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) { liveVm.poll(config?.activeContext to invalidations) }
    }

    // Android 17: a cluster on the Wi-Fi network needs local network access, asked up front.
    AskLocalNetworkAccess(config?.activeSummary, onGranted = vm.talos::invalidate)

    // Members the talosconfig misses, once the overview loaded (its nodes answer, so will discovery).
    val discovered by discoveryVm.offer.collectAsStateWithLifecycle()
    var showDiscovered by remember { mutableStateOf(false) }
    LaunchedEffect(loaded, config?.activeContext, invalidations) {
        if (loaded) discoveryVm.discover() else discoveryVm.clear()
    }

    Scaffold(
        bottomBar = { DataFreshness(state) },
        // The AI diagnosis is optional: no trace of it unless it was turned on in Settings.
        floatingActionButton = {
            if (ai.enabled) {
                SmallFloatingActionButton(onClick = onDiagnose) {
                    Icon(Icons.Outlined.AutoAwesome, stringResource(R.string.ai_title))
                }
            }
        },
        topBar = {
            TopAppBar(
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
                    // Only offered when the config's role can run it.
                    if (config?.activeSummary?.allows(Feature.HEALTH) == true) {
                        TooltipIconButton(Icons.Outlined.Favorite, stringResource(R.string.overview_action_health), onClick = onHealth)
                    }
                    // Cluster-wide screens: only disabled when no reachable node's Talos has them.
                    val reachable = (state as? UiState.Loaded)?.data?.nodes?.filter { it.reachable }?.map { it.node }
                    val features = rememberClusterFeatures(reachable)
                    TooltipIconButton(
                        Icons.Outlined.Timeline,
                        stringResource(R.string.overview_action_events),
                        onClick = onEvents,
                        enabled = clusterSupport(features, TalosFeature.EVENTS).supported,
                    )
                    // Kubernetes workloads: the API is reached with the admin kubeconfig Talos issues.
                    if (config?.activeSummary?.allows(Feature.WORKLOADS) == true) {
                        TooltipIconButton(Icons.Outlined.Widgets, stringResource(R.string.overview_action_workloads), onClick = onWorkloads)
                    }
                    TooltipIconButton(
                        Icons.Outlined.Hub,
                        "KubeSpan",
                        onClick = onKubeSpan,
                        enabled = clusterSupport(features, TalosFeature.KUBESPAN).supported,
                    )
                    TooltipIconButton(
                        Icons.Outlined.Storage,
                        "etcd",
                        onClick = onEtcd,
                        enabled = clusterSupport(features, TalosFeature.ETCD).supported,
                    )
                    TooltipIconButton(Icons.Outlined.Settings, stringResource(R.string.overview_action_settings), onClick = onSettings)
                },
            )
        },
    ) { padding ->
        config?.takeIf { showClusters }?.let { stored ->
            ClusterSheet(
                config = stored,
                colors = clusterColors,
                labels = clusterLabels,
                onSelect = {
                    showClusters = false
                    app.selectCluster(it)
                },
                onRename = { cluster, name -> app.renameCluster(cluster.fingerprint, name) },
                onColor = { cluster, color -> app.clusterColors.set(cluster.fingerprint, color) },
                vpnOnly = vpnOnly,
                onVpnOnly = { cluster, on -> app.setVpnOnly(cluster.fingerprint, on) },
                onAdd = {
                    showClusters = false
                    onAddCluster()
                },
                onRemove = { name ->
                    scope.launch {
                        runCatching { app.removeCluster(name) }.fold(
                            onSuccess = { remains -> if (!remains) onClustersCleared() },
                            onFailure = { Toast.makeText(context, it.userMessage(), Toast.LENGTH_LONG).show() },
                        )
                    }
                },
                onDismiss = { showClusters = false },
                onEndpoints = {
                    showClusters = false
                    editingEndpoints = it.name
                },
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
        when (val s = state) {
            UiState.Loading -> LoadingBox(Modifier.padding(padding))
            is UiState.Failed -> ErrorBox(s.message, vm::refresh, Modifier.padding(padding))
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
                modifier = Modifier.padding(padding).fillMaxSize(),
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
                    onNode = onNode,
                    onSettings = onSettings,
                    onNodeAction = onNodeAction,
                    canPower = config?.activeSummary?.allows(Feature.POWER) == true,
                    canShell = config?.activeSummary?.allows(Feature.DEBUG_SHELL) == true,
                    canUpgrade = config?.activeSummary?.allows(Feature.UPGRADE) == true,
                    onUpgrade = onUpgrade,
                    live = liveState.takeIf { liveEnabled },
                    discovered = discovered.size,
                    onDiscovered = { showDiscovered = true },
                )
            }
        }
    }
}

@Composable
private fun NodeList(
    overview: ClusterOverview,
    outage: ClusterOutage?,
    onRetry: () -> Unit,
    clusterName: String?,
    fingerprint: String?,
    time: UiState<ClusterTime>,
    certificate: ContextSummary?,
    onIssueConfig: () -> Unit,
    onInsights: () -> Unit,
    apps: UiState<Inventory>,
    onApps: () -> Unit,
    dataServices: UiState<DataServices>?,
    dataHints: String,
    onDataServices: () -> Unit,
    onNode: (NodeOverview) -> Unit,
    onSettings: () -> Unit,
    onNodeAction: (NodeOverview, NodeAction) -> Unit,
    canPower: Boolean,
    canShell: Boolean,
    canUpgrade: Boolean,
    onUpgrade: (NodeOverview, String) -> Unit,
    live: ClusterLiveState?,
    discovered: Int,
    onDiscovered: () -> Unit,
) {
    var sheetFor by remember { mutableStateOf<NodeOverview?>(null) }
    val wakeOnLan = rememberWakeOnLan(fingerprint)
    RecordNodeMacs(fingerprint, overview.nodes)
    sheetFor?.let { node ->
        NodeActionsSheet(
            node = node,
            canPower = canPower,
            canShell = canShell,
            wol = wakeOnLan(node),
            onAction = { onNodeAction(node, it) },
            onDismiss = { sheetFor = null },
        )
    }
    val nodes = overview.nodes.sortedWith(compareBy({ it.role != "controlplane" }, { it.hostname }))
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
        if (BuildConfig.SELF_UPDATE) item { UpdateBanner(onClick = onSettings) }
        if (BuildConfig.DONATIONS) item { SupportCard() }
        certificate?.let { item { CertificateBanner(it, onIssueConfig) } }
        if (discovered > 0) item { DiscoveredNodesBanner(discovered, onDiscovered) }
        if (certificate?.isDemo != true) item { TalosUpdateBanner(overview.nodes, canUpgrade, onUpgrade) }
        item { ClusterSummaryCard(clusterName ?: overview.context, clusterSummary(overview.nodes), live, onInsights) }
        item { AppsCard(apps, onApps) }
        if (dataServices != null) item {
            val inventory = (apps as? UiState.Loaded)?.data
            val appsById = remember(inventory) { inventory?.apps.orEmpty().associateBy { it.id } }
            val hinted = remember(dataHints) { DataServiceKind.entries.filter { it.catalogId in dataHints.split(',') } }
            val downNodes = remember(overview) { overview.downHostnames() }
            DataServicesCard(dataServices, hinted, appsById, downNodes, onDataServices)
        }
        if (nodes.isNotEmpty()) item(key = "nodes") {
            NodesCard(
                nodes,
                onNode = onNode,
                onLive = { onNodeAction(it, NodeAction.LIVE) },
                onMore = { sheetFor = it },
            )
        }
        // After the nodes: they come first, the clocks are a secondary check.
        item { TimeDriftCard(time, overview.nodes.associate { it.node to it.hostname }) }
    }
}

/** The nodes as one card, like Apps and Data services: a title, then a swipeable row per node. */
@Composable
private fun NodesCard(
    nodes: List<NodeOverview>,
    onNode: (NodeOverview) -> Unit,
    onLive: (NodeOverview) -> Unit,
    onMore: (NodeOverview) -> Unit,
) {
    Card(Modifier.fillMaxWidth()) {
        Row(Modifier.padding(start = 16.dp, end = 16.dp, top = 16.dp, bottom = 4.dp), verticalAlignment = Alignment.CenterVertically) {
            Text(stringResource(R.string.overview_stat_nodes), style = MaterialTheme.typography.titleMedium, modifier = Modifier.weight(1f))
            Text(nodes.size.toString(), style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        nodes.forEachIndexed { i, node ->
            if (i > 0) HorizontalDivider(Modifier.padding(horizontal = 16.dp), color = MaterialTheme.colorScheme.outlineVariant)
            SwipeableNode(node, onLive = { onLive(node) }, onMore = { onMore(node) }) {
                NodeRow(node, onClick = { onNode(node) }, onLongClick = { onMore(node) })
            }
        }
        Spacer(Modifier.height(4.dp))
    }
}

@Composable
private fun NodeRow(node: NodeOverview, onClick: () -> Unit, onLongClick: () -> Unit) {
    // Opaque, so the swipe background only shows beside the row as it slides.
    Box(
        Modifier.fillMaxWidth().background(CardDefaults.cardColors().containerColor).combinedClickable(
            onClick = { if (node.reachable) onClick() },
            onLongClick = onLongClick,
            onLongClickLabel = stringResource(R.string.overview_node_actions),
        ),
    ) {
        Column(Modifier.padding(horizontal = 16.dp, vertical = 12.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) {
                    Text(node.hostname, style = MaterialTheme.typography.titleMedium)
                    Text(
                        node.node,
                        style = MaterialTheme.typography.bodySmall,
                        fontFamily = FontFamily.Monospace,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                NodeHealthPill(node.health)
            }
            if (node.reachable) {
                Spacer(Modifier.width(4.dp))
                Text(
                    listOf(roleLabel(node.role), node.version, node.stage, node.arch)
                        .filter { it.isNotBlank() }
                        .joinToString("  ·  "),
                    style = MaterialTheme.typography.bodySmall,
                    modifier = Modifier.padding(top = 8.dp),
                )
            }
            // No longer answering: what it was when it last did, dimmed.
            node.lastSeen?.let { seen ->
                Text(
                    listOf(roleLabel(node.role), node.version, node.arch).filter { it.isNotBlank() }.joinToString("  ·  "),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.padding(top = 8.dp),
                )
                Text(
                    stringResource(R.string.overview_node_last_seen, agoLabel(System.currentTimeMillis() - seen)),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            node.unmetConditions.forEach {
                Text(
                    "${it.name}: ${it.reason}",
                    style = MaterialTheme.typography.bodySmall,
                    color = LocalStatusColors.current.warn,
                    modifier = Modifier.padding(top = 4.dp),
                )
            }
            node.error?.let {
                Text(
                    it,
                    style = MaterialTheme.typography.bodySmall,
                    color = LocalStatusColors.current.bad,
                    modifier = Modifier.padding(top = 4.dp),
                )
            }
        }
    }
}

@Composable
private fun roleLabel(role: String) = when (role) {
    "controlplane" -> stringResource(R.string.overview_role_control_plane)
    else -> role
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
            Text(
                stringResource(if (canRenew) R.string.overview_cert_renew else R.string.overview_cert_ask_admin),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
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
private fun ScreenshotModeIcon() {
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
private fun UpdateBanner(onClick: () -> Unit) {
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

/** Occasional, dismissable ask to support the project (see SupportPrompt for the timing). */
@Composable
private fun SupportCard() {
    val context = LocalContext.current
    val prompt = (context.applicationContext as TalosApp).supportPrompt
    val visible by prompt.visible.collectAsStateWithLifecycle()
    if (!visible) return
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp)) {
            Text(stringResource(R.string.overview_support_title), style = MaterialTheme.typography.titleSmall)
            Text(
                stringResource(R.string.overview_support_body),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Row(horizontalArrangement = Arrangement.End, modifier = Modifier.fillMaxWidth()) {
                TextButton(onClick = prompt::never) { Text(stringResource(R.string.overview_support_never)) }
                TextButton(onClick = prompt::later) { Text(stringResource(R.string.overview_support_later)) }
                TextButton(onClick = {
                    prompt.later()
                    openUrl(context, SPONSOR_URL)
                }) { Text(stringResource(R.string.overview_support_sponsor)) }
            }
        }
    }
}
