package name.levis.talosmobile.ui.node

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material3.Card
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Tab
import androidx.compose.material3.TabRow
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.talosmobile.data.TalosRepository
import name.levis.talosmobile.data.servicesKey
import name.levis.talosmobile.data.resourcesKey
import name.levis.talosmobile.model.NodeResources
import name.levis.talosmobile.model.ServiceInfo
import name.levis.talosmobile.ui.LoadingViewModel
import name.levis.talosmobile.ui.UiState
import name.levis.talosmobile.ui.app
import name.levis.talosmobile.ui.components.ErrorBox
import name.levis.talosmobile.ui.components.InfoRow
import name.levis.talosmobile.ui.components.LoadingBox
import name.levis.talosmobile.ui.components.SectionTitle
import name.levis.talosmobile.ui.components.StatusPill
import name.levis.talosmobile.ui.components.UsageBar
import name.levis.talosmobile.ui.factory
import name.levis.talosmobile.ui.theme.LocalStatusColors
import name.levis.talosmobile.util.formatBytes
import name.levis.talosmobile.util.formatDuration
import name.levis.talosmobile.util.usedFraction
import java.util.Locale
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.size
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
import name.levis.talosmobile.TalosApp
import name.levis.talosmobile.ui.live.LiveStatsTab
import name.levis.talosmobile.data.activeSummary
import name.levis.talosmobile.model.Feature
import name.levis.talosmobile.model.allows
import name.levis.talosmobile.security.AuthResult
import name.levis.talosmobile.security.authenticate
import name.levis.talosmobile.security.findFragmentActivity
import kotlinx.coroutines.launch

class ServicesViewModel(private val talos: TalosRepository, private val node: String) : LoadingViewModel<List<ServiceInfo>>() {
    override fun cached(): List<ServiceInfo>? = talos.cached(servicesKey(node))
    override suspend fun fetch() = talos.services(node)
}

class ResourcesViewModel(private val talos: TalosRepository, private val node: String) : LoadingViewModel<NodeResources>() {
    override fun cached(): NodeResources? = talos.cached(resourcesKey(node))
    override suspend fun fetch() = talos.resources(node)
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun NodeDetailScreen(
    node: String,
    hostname: String,
    role: String,
    onBack: () -> Unit,
    onLogs: (service: String?) -> Unit,
    power: PowerViewModel = viewModel(key = "power-$node", factory = factory { PowerViewModel(app.talosRepository, node) }),
) {
    var tab by rememberSaveable { mutableIntStateOf(0) }
    var menuOpen by remember { mutableStateOf(false) }
    var confirming by remember { mutableStateOf<PowerAction?>(null) }
    val powerState by power.state.collectAsStateWithLifecycle()
    val snackbar = remember { SnackbarHostState() }
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val app = context.applicationContext as TalosApp
    val appLock = app.appLock
    val config by app.configRepository.config.collectAsStateWithLifecycle()
    val canPower = config?.activeSummary?.allows(Feature.POWER) ?: true

    LaunchedEffect(powerState) {
        when (val s = powerState) {
            is PowerState.Done -> {
                snackbar.showSnackbar("$hostname: ${s.request.title.lowercase()} requested")
                power.dismiss()
                onBack()
            }
            is PowerState.Failed -> {
                snackbar.showSnackbar(s.message)
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
            when (val auth = authenticate(activity, "${request.title} $hostname")) {
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
                    IconButton(onClick = onBack) { Icon(Icons.AutoMirrored.Filled.ArrowBack, "Back") }
                },
                actions = {
                    if (powerState is PowerState.Running) {
                        CircularProgressIndicator(Modifier.size(20.dp).padding(end = 4.dp), strokeWidth = 2.dp)
                    }
                    Box {
                        IconButton(onClick = { menuOpen = true }) { Icon(Icons.Outlined.MoreVert, "More") }
                        DropdownMenu(expanded = menuOpen, onDismissRequest = { menuOpen = false }) {
                            DropdownMenuItem(
                                text = { Text("Kernel log") },
                                leadingIcon = { Icon(Icons.Outlined.Terminal, contentDescription = null) },
                                onClick = {
                                    menuOpen = false
                                    onLogs(null)
                                },
                            )
                            // Power actions only exist for configs whose role allows them.
                            if (canPower) {
                                HorizontalDivider()
                                PowerAction.entries.forEach { action ->
                                    DropdownMenuItem(
                                        text = { Text(action.title) },
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
            TabRow(selectedTabIndex = tab) {
                Tab(selected = tab == 0, onClick = { tab = 0 }, text = { Text("Services") })
                Tab(selected = tab == 1, onClick = { tab = 1 }, text = { Text("Resources") })
                Tab(selected = tab == 2, onClick = { tab = 2 }, text = { Text("Live") })
            }
            when (tab) {
                0 -> ServicesTab(node, onService = { onLogs(it) })
                1 -> ResourcesTab(node)
                else -> LiveStatsTab(node)
            }
        }
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
private fun ServicesTab(
    node: String,
    onService: (String) -> Unit,
    vm: ServicesViewModel = viewModel(key = "services-$node", factory = factory { ServicesViewModel(app.talosRepository, node) }),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { if (state == UiState.Loading) vm.refresh() }

    when (val s = state) {
        UiState.Loading -> LoadingBox()
        is UiState.Failed -> ErrorBox(s.message, vm::refresh)
        is UiState.Loaded -> PullToRefreshBox(isRefreshing = s.refreshing, onRefresh = vm::refresh, modifier = Modifier.fillMaxSize()) {
            LazyColumn(contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                items(s.data, key = { it.id }) { ServiceRow(it, onClick = { onService(it.id) }) }
            }
        }
    }
}

@Composable
private fun ServiceRow(svc: ServiceInfo, onClick: () -> Unit) {
    val colors = LocalStatusColors.current
    Card(onClick = onClick, modifier = Modifier.fillMaxWidth()) {
        Column(Modifier.padding(horizontal = 16.dp, vertical = 12.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) {
                    Text(svc.id, style = MaterialTheme.typography.titleSmall)
                    Text(svc.state, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
                when (svc.health) {
                    "healthy" -> StatusPill("Healthy", colors.ok)
                    "unhealthy" -> StatusPill("Unhealthy", colors.bad)
                    else -> StatusPill("No check", colors.muted)
                }
            }
            val detail = svc.message?.takeIf { svc.health == "unhealthy" } ?: svc.lastEvent
            detail?.takeIf { it.isNotBlank() }?.let {
                Text(
                    it,
                    style = MaterialTheme.typography.bodySmall,
                    color = if (svc.health == "unhealthy") colors.bad else MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.padding(top = 6.dp),
                )
            }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun ResourcesTab(
    node: String,
    vm: ResourcesViewModel = viewModel(key = "resources-$node", factory = factory { ResourcesViewModel(app.talosRepository, node) }),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { if (state == UiState.Loading) vm.refresh() }

    when (val s = state) {
        UiState.Loading -> LoadingBox()
        is UiState.Failed -> ErrorBox(s.message, vm::refresh)
        is UiState.Loaded -> PullToRefreshBox(isRefreshing = s.refreshing, onRefresh = vm::refresh, modifier = Modifier.fillMaxSize()) {
            ResourcesContent(s.data)
        }
    }
}

@Composable
private fun ResourcesContent(r: NodeResources) {
    val uptime = if (r.bootTime > 0) formatDuration(System.currentTimeMillis() / 1000 - r.bootTime) else "—"
    LazyColumn(contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        item {
            Card(Modifier.fillMaxWidth()) {
                Column(Modifier.padding(16.dp)) {
                    SectionTitle("System")
                    InfoRow("Uptime", uptime)
                    InfoRow("CPU", if (r.cpuModel.isBlank()) "${r.cpuCount} threads" else "${r.cpuCount} × ${r.cpuModel}")
                    InfoRow("Load (1/5/15)", String.format(Locale.ROOT, "%.2f  %.2f  %.2f", r.load1, r.load5, r.load15))
                    if (r.cpuCount > 0) UsageBar((r.load1 / r.cpuCount).toFloat().coerceIn(0f, 1f), Modifier.padding(top = 4.dp))
                }
            }
        }
        item {
            Card(Modifier.fillMaxWidth()) {
                Column(Modifier.padding(16.dp)) {
                    SectionTitle("Memory")
                    val used = r.memTotal - r.memAvailable
                    InfoRow("Used", "${formatBytes(used)} / ${formatBytes(r.memTotal)}")
                    UsageBar(usedFraction(r.memTotal, r.memAvailable), Modifier.padding(top = 4.dp))
                }
            }
        }
        if (r.mounts.isNotEmpty()) {
            item {
                Card(Modifier.fillMaxWidth()) {
                    Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                        SectionTitle("Disks")
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
