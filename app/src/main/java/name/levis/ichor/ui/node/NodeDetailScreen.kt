package name.levis.ichor.ui.node

import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import name.levis.ichor.R
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material3.Card
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Tab
import androidx.compose.material3.PrimaryScrollableTabRow
import androidx.compose.material.icons.outlined.Timeline
import kotlinx.coroutines.delay
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.data.servicesKey
import name.levis.ichor.data.resourcesKey
import name.levis.ichor.model.NodeResources
import name.levis.ichor.model.NodeTime
import name.levis.ichor.model.ServiceInfo
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.DataFreshness
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.InfoRow
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.UsageBar
import name.levis.ichor.ui.factory
import name.levis.ichor.util.formatBytes
import name.levis.ichor.ui.components.localizedDuration
import name.levis.ichor.util.usedFraction
import java.util.Locale
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.outlined.Description
import androidx.compose.material.icons.outlined.FolderOpen
import androidx.compose.material.icons.outlined.NetworkCheck
import androidx.compose.material.icons.outlined.SystemUpdateAlt
import androidx.compose.material.icons.outlined.Lan
import androidx.compose.material.icons.outlined.Layers
import androidx.compose.material.icons.outlined.Memory
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material.icons.outlined.PowerSettingsNew
import androidx.compose.material.icons.outlined.Terminal
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.platform.LocalContext
import name.levis.ichor.TalosApp
import name.levis.ichor.ui.live.LiveStatsTab
import name.levis.ichor.data.activeSummary
import name.levis.ichor.model.ContainerInfo
import name.levis.ichor.model.Feature
import name.levis.ichor.model.TalosFeature
import name.levis.ichor.model.notice
import name.levis.ichor.model.support
import name.levis.ichor.ui.components.FeatureGate
import name.levis.ichor.ui.components.rememberNodeFeatures
import name.levis.ichor.model.allows
import name.levis.ichor.security.AuthResult
import name.levis.ichor.security.authenticate
import name.levis.ichor.security.findFragmentActivity
import kotlinx.coroutines.launch

/** How long after a service action the list is fetched again, once the state settled. */
private const val SERVICE_SETTLE_MILLIS = 2_500L

class ServicesViewModel(private val talos: TalosRepository, private val node: String) : LoadingViewModel<List<ServiceInfo>>() {
    override fun cached(): TalosRepository.Timed<List<ServiceInfo>>? = talos.cached(servicesKey(node))
    override suspend fun fetch() = talos.services(node)
}

class ResourcesViewModel(private val talos: TalosRepository, private val node: String) : LoadingViewModel<NodeResources>() {
    override fun cached(): TalosRepository.Timed<NodeResources>? = talos.cached(resourcesKey(node))
    override suspend fun fetch() = talos.resources(node)
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun NodeDetailScreen(
    node: String,
    hostname: String,
    role: String,
    onBack: () -> Unit,
    initialTab: Int = 0,
    initialAction: PowerAction? = null,
    onLogs: (service: String?) -> Unit,
    onContainerLogs: (ContainerInfo) -> Unit,
    onMenu: (NodeMenuEntry) -> Unit,
    power: PowerViewModel = viewModel(key = "power-$node", factory = factory { PowerViewModel(app.talosRepository, node) }),
) {
    var tab by rememberSaveable { mutableIntStateOf(initialTab) }
    var menuOpen by remember { mutableStateOf(false) }
    var confirming by remember { mutableStateOf<PowerAction?>(null) }
    // A reboot/shutdown started from the overview opens its confirmation once (not again on rotation).
    var initialActionShown by rememberSaveable { mutableStateOf(false) }
    if (!initialActionShown && initialAction != null) {
        initialActionShown = true
        confirming = initialAction
    }
    val powerState by power.state.collectAsStateWithLifecycle()
    val snackbar = remember { SnackbarHostState() }
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val app = context.applicationContext as TalosApp
    val appLock = app.appLock
    val config by app.configRepository.config.collectAsStateWithLifecycle()
    val canPower = config?.activeSummary?.allows(Feature.POWER) ?: false
    val canControlServices = config?.activeSummary?.allows(Feature.SERVICE_CONTROL) ?: false
    val features = rememberNodeFeatures(node)
    // One upgrade at a time in the app: the entry stays open for the node being upgraded.
    val upgrading by app.upgradeManager.current.collectAsStateWithLifecycle()
    val upgradeBusyElsewhere = upgrading?.let { it.running && it.node != node } ?: false
    val serviceControl: ServiceControlViewModel = viewModel(
        key = "service-control-$node",
        factory = factory { ServiceControlViewModel(app.talosRepository, node) },
    )
    val services: ServicesViewModel = viewModel(key = "services-$node", factory = factory { ServicesViewModel(app.talosRepository, node) })
    val controlState by serviceControl.state.collectAsStateWithLifecycle()
    var confirmingService by remember { mutableStateOf<ServiceRequest?>(null) }

    // dismiss() comes last: it changes the effect's key, which cancels whatever still runs here.
    LaunchedEffect(controlState) {
        when (val s = controlState) {
            is ServiceControlState.Done -> {
                services.refresh()
                // Talos applies the action asynchronously: look again once it had time to settle
                // (in the screen's scope, so another action does not cancel it).
                scope.launch {
                    delay(SERVICE_SETTLE_MILLIS)
                    services.refresh()
                }
                snackbar.showSnackbar(context.getString(s.request.action.done, s.request.service, hostname))
                serviceControl.dismiss()
            }
            is ServiceControlState.Failed -> {
                snackbar.showSnackbar(
                    context.getString(
                        R.string.service_action_failed,
                        context.getString(s.request.action.label),
                        s.request.service,
                        s.message.resolve(context),
                    ),
                )
                serviceControl.dismiss()
            }
            else -> Unit
        }
    }

    // Like power actions: with the app lock on, a fresh fingerprint/PIN first.
    fun serviceConfirmed(request: ServiceRequest) {
        confirmingService = null
        val activity = context.findFragmentActivity()
        if (!appLock.enabled.value || activity == null) {
            serviceControl.run(request)
            return
        }
        scope.launch {
            val title = context.getString(request.action.label)
            when (val auth = authenticate(activity, title, "${request.service} · $hostname")) {
                AuthResult.Success -> serviceControl.run(request)
                is AuthResult.Failure -> snackbar.showSnackbar(auth.message)
            }
        }
    }

    LaunchedEffect(powerState) {
        when (val s = powerState) {
            is PowerState.Done -> {
                snackbar.showSnackbar(context.getString(R.string.power_requested, hostname, context.getString(s.request.title)))
                power.dismiss()
                onBack()
            }
            is PowerState.Failed -> {
                snackbar.showSnackbar(s.message.resolve(context))
                power.dismiss()
            }
            else -> Unit
        }
    }

    // With the app lock on, destructive actions need a fresh fingerprint/PIN.
    fun confirmed(request: PowerRequest) {
        confirming = null
        val activity = context.findFragmentActivity()
        if (!appLock.enabled.value || activity == null) {
            power.run(request)
            return
        }
        scope.launch {
            when (val auth = authenticate(activity, context.getString(R.string.power_auth_title, context.getString(request.title), hostname))) {
                AuthResult.Success -> power.run(request)
                is AuthResult.Failure -> snackbar.showSnackbar(auth.message)
            }
        }
    }

    Scaffold(
        snackbarHost = { SnackbarHost(snackbar) },
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text(hostname)
                        Text(node, style = MaterialTheme.typography.labelMedium, fontFamily = FontFamily.Monospace)
                    }
                },
                navigationIcon = {
                    IconButton(onClick = onBack) { Icon(Icons.AutoMirrored.Filled.ArrowBack, stringResource(R.string.common_back)) }
                },
                actions = {
                    if (powerState is PowerState.Running || controlState is ServiceControlState.Running) {
                        CircularProgressIndicator(Modifier.size(20.dp).padding(end = 4.dp), strokeWidth = 2.dp)
                    }
                    Box {
                        IconButton(onClick = { menuOpen = true }) { Icon(Icons.Outlined.MoreVert, stringResource(R.string.common_more)) }
                        DropdownMenu(expanded = menuOpen, onDismissRequest = { menuOpen = false }) {
                            NodeMenuItems(
                                summary = config?.activeSummary,
                                features = features,
                                // One upgrade at a time in the app.
                                busy = if (upgradeBusyElsewhere) setOf(NodeMenuEntry.UPGRADE) else emptySet(),
                                onPick = { entry ->
                                    menuOpen = false
                                    onMenu(entry)
                                },
                            )
                            // Power actions only exist for configs whose role allows them.
                            if (canPower) {
                                HorizontalDivider()
                                PowerAction.entries.forEach { action ->
                                    DropdownMenuItem(
                                        text = { Text(stringResource(action.title)) },
                                        leadingIcon = { Icon(Icons.Outlined.PowerSettingsNew, contentDescription = null) },
                                        enabled = powerState !is PowerState.Running,
                                        onClick = {
                                            menuOpen = false
                                            confirming = action
                                        },
                                    )
                                }
                            }
                        }
                    }
                },
            )
        },
    ) { padding ->
        Column(Modifier.padding(padding).fillMaxSize()) {
            PrimaryScrollableTabRow(selectedTabIndex = tab, edgePadding = 0.dp) {
                Tab(selected = tab == 0, onClick = { tab = 0 }, text = { Text(stringResource(R.string.node_tab_services)) })
                Tab(selected = tab == 1, onClick = { tab = 1 }, text = { Text(stringResource(R.string.node_tab_resources)) })
                Tab(selected = tab == 2, onClick = { tab = 2 }, text = { Text(stringResource(R.string.node_tab_live)) })
                // Tabs this Talos version lacks stay reachable (dimmed): they say what they need.
                val dimmed = MaterialTheme.colorScheme.onSurfaceVariant.copy(alpha = 0.5f)
                Tab(
                    selected = tab == 3,
                    onClick = { tab = 3 },
                    text = { Text(stringResource(R.string.node_tab_processes)) },
                    unselectedContentColor = if (features.support(TalosFeature.PROCESSES).supported) MaterialTheme.colorScheme.onSurfaceVariant else dimmed,
                )
                Tab(
                    selected = tab == 4,
                    onClick = { tab = 4 },
                    text = { Text(stringResource(R.string.node_tab_pods)) },
                    unselectedContentColor = if (features.support(TalosFeature.CONTAINERS).supported) MaterialTheme.colorScheme.onSurfaceVariant else dimmed,
                )
            }
            when (tab) {
                0 -> ServicesTab(
                    node,
                    onService = { onLogs(it) },
                    onAction = if (canControlServices) ({ confirmingService = it }) else null,
                    busy = controlState is ServiceControlState.Running,
                    actionsNotice = features.support(TalosFeature.SERVICE_CONTROL).notice,
                    vm = services,
                )
                1 -> ResourcesTab(node)
                2 -> LiveStatsTab(node)
                3 -> FeatureGate(features.support(TalosFeature.PROCESSES)) { ProcessesTab(node) }
                else -> FeatureGate(features.support(TalosFeature.CONTAINERS)) { PodsTab(node, onContainer = onContainerLogs) }
            }
        }
    }

    confirmingService?.let { request ->
        ServiceConfirmDialog(
            request = request,
            hostname = hostname,
            onConfirm = { serviceConfirmed(request) },
            onDismiss = { confirmingService = null },
        )
    }

    confirming?.let { action ->
        PowerConfirmDialog(
            action = action,
            hostname = hostname,
            role = role,
            onConfirm = ::confirmed,
            onDismiss = { confirming = null },
        )
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun ResourcesTab(
    node: String,
    vm: ResourcesViewModel = viewModel(key = "resources-$node", factory = factory { ResourcesViewModel(app.talosRepository, node) }),
    clock: NodeTimeViewModel = viewModel(key = "time-$node", factory = factory { NodeTimeViewModel(app.talosRepository, node) }),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    val clockState by clock.state.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) {
        if (state == UiState.Loading) vm.refresh()
        if (clockState == UiState.Loading) clock.refresh()
    }

    when (val s = state) {
        UiState.Loading -> LoadingBox()
        is UiState.Failed -> ErrorBox(s.message, vm::refresh)
        is UiState.Loaded -> Column(Modifier.fillMaxSize()) {
            PullToRefreshBox(
                isRefreshing = s.refreshing,
                onRefresh = {
                    vm.refresh()
                    clock.refresh()
                },
                modifier = Modifier.weight(1f),
            ) {
                ResourcesContent(s.data, clockState)
            }
            DataFreshness(s, edgeToEdge = false)
        }
    }
}

@Composable
private fun ResourcesContent(r: NodeResources, clock: UiState<NodeTime>) {
    val uptime = if (r.bootTime > 0) localizedDuration(System.currentTimeMillis() / 1000 - r.bootTime) else "—"
    val cpu = if (r.cpuModel.isBlank()) {
        pluralStringResource(R.plurals.node_cpu_threads, r.cpuCount, r.cpuCount)
    } else {
        "${r.cpuCount} × ${r.cpuModel}"
    }
    LazyColumn(contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        item {
            Card(Modifier.fillMaxWidth()) {
                Column(Modifier.padding(16.dp)) {
                    SectionTitle(stringResource(R.string.node_section_system))
                    InfoRow(stringResource(R.string.node_uptime), uptime)
                    InfoRow(stringResource(R.string.node_cpu), cpu)
                    InfoRow(stringResource(R.string.node_load), String.format(Locale.ROOT, "%.2f  %.2f  %.2f", r.load1, r.load5, r.load15))
                    if (r.cpuCount > 0) UsageBar((r.load1 / r.cpuCount).toFloat().coerceIn(0f, 1f), Modifier.padding(top = 4.dp))
                    ClockOffsetRow(clock)
                }
            }
        }
        item {
            Card(Modifier.fillMaxWidth()) {
                Column(Modifier.padding(16.dp)) {
                    SectionTitle(stringResource(R.string.node_section_memory))
                    val used = r.memTotal - r.memAvailable
                    InfoRow(stringResource(R.string.node_memory_used), "${formatBytes(used)} / ${formatBytes(r.memTotal)}")
                    UsageBar(usedFraction(r.memTotal, r.memAvailable), Modifier.padding(top = 4.dp))
                }
            }
        }
        if (r.mounts.isNotEmpty()) {
            item {
                Card(Modifier.fillMaxWidth()) {
                    Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                        SectionTitle(stringResource(R.string.node_section_disks))
                        r.mounts.forEach { m ->
                            Column {
                                Row {
                                    Text(m.mountedOn, style = MaterialTheme.typography.bodyMedium, modifier = Modifier.weight(1f))
                                    Text(
                                        "${formatBytes(m.size - m.available)} / ${formatBytes(m.size)}",
                                        style = MaterialTheme.typography.bodySmall,
                                    )
                                }
                                Text(
                                    m.filesystem,
                                    style = MaterialTheme.typography.bodySmall,
                                    fontFamily = FontFamily.Monospace,
                                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                                )
                                UsageBar(usedFraction(m.size, m.available), Modifier.padding(top = 4.dp))
                            }
                        }
                    }
                }
            }
        }
    }
}

