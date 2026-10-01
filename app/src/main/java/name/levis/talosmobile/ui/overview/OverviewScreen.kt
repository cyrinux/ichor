package name.levis.talosmobile.ui.overview

import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Favorite
import androidx.compose.material.icons.outlined.Hub
import androidx.compose.material.icons.outlined.Settings
import androidx.compose.material.icons.outlined.Storage
import androidx.compose.material3.Card
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import androidx.compose.ui.platform.LocalContext
import name.levis.talosmobile.TalosApp
import androidx.compose.material3.TextButton
import name.levis.talosmobile.data.ConfigRepository
import name.levis.talosmobile.data.SPONSOR_URL
import name.levis.talosmobile.ui.settings.openUrl
import name.levis.talosmobile.data.activeSummary
import name.levis.talosmobile.model.Feature
import name.levis.talosmobile.model.accessLabel
import name.levis.talosmobile.model.allows
import name.levis.talosmobile.update.UpdateState
import name.levis.talosmobile.data.TalosRepository
import name.levis.talosmobile.data.OVERVIEW
import name.levis.talosmobile.model.ClusterOverview
import name.levis.talosmobile.model.NodeHealth
import name.levis.talosmobile.model.NodeOverview
import name.levis.talosmobile.model.health
import name.levis.talosmobile.ui.LoadingViewModel
import name.levis.talosmobile.ui.UiState
import name.levis.talosmobile.ui.app
import name.levis.talosmobile.ui.components.DataFreshness
import name.levis.talosmobile.ui.components.ErrorBox
import name.levis.talosmobile.ui.components.LoadingBox
import name.levis.talosmobile.ui.components.NodeHealthPill
import name.levis.talosmobile.ui.factory
import name.levis.talosmobile.ui.theme.LocalStatusColors

class OverviewViewModel(
    private val talos: TalosRepository,
    val configs: ConfigRepository,
) : LoadingViewModel<ClusterOverview>() {
    override fun cached(): TalosRepository.Timed<ClusterOverview>? = talos.cached(OVERVIEW)
    override suspend fun fetch() = talos.overview()
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun OverviewScreen(
    onNode: (NodeOverview) -> Unit,
    onNodeAction: (NodeOverview, NodeAction) -> Unit,
    onEtcd: () -> Unit,
    onKubeSpan: () -> Unit,
    onHealth: () -> Unit,
    onSettings: () -> Unit,
    vm: OverviewViewModel = viewModel(factory = factory { OverviewViewModel(app.talosRepository, app.configRepository) }),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    val config by vm.configs.config.collectAsStateWithLifecycle()

    // Reload whenever the active context changes (including first composition).
    LaunchedEffect(config?.activeContext) { vm.refresh(reset = true) }

    Scaffold(
        bottomBar = { DataFreshness(state) },
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text("Cluster")
                        config?.let { stored ->
                            val access = stored.activeSummary?.accessLabel
                            Text(
                                listOfNotNull(stored.activeContext, access).joinToString(" · "),
                                style = MaterialTheme.typography.labelMedium,
                                color = MaterialTheme.colorScheme.primary,
                            )
                        }
                    }
                },
                actions = {
                    // Only offered when the config's role can run it.
                    if (config?.activeSummary?.allows(Feature.HEALTH) == true) {
                        IconButton(onClick = onHealth) { Icon(Icons.Outlined.Favorite, "Cluster health") }
                    }
                    IconButton(onClick = onKubeSpan) { Icon(Icons.Outlined.Hub, "KubeSpan") }
                    IconButton(onClick = onEtcd) { Icon(Icons.Outlined.Storage, "etcd") }
                    IconButton(onClick = onSettings) { Icon(Icons.Outlined.Settings, "Settings") }
                },
            )
        },
    ) { padding ->
        when (val s = state) {
            UiState.Loading -> LoadingBox(Modifier.padding(padding))
            is UiState.Failed -> ErrorBox(s.message, vm::refresh, Modifier.padding(padding))
            is UiState.Loaded -> PullToRefreshBox(
                isRefreshing = s.refreshing,
                onRefresh = vm::refresh,
                modifier = Modifier.padding(padding).fillMaxSize(),
            ) {
                NodeList(
                    overview = s.data,
                    onNode = onNode,
                    onSettings = onSettings,
                    onNodeAction = onNodeAction,
                    canPower = config?.activeSummary?.allows(Feature.POWER) == true,
                    canShell = config?.activeSummary?.allows(Feature.DEBUG_SHELL) == true,
                )
            }
        }
    }
}

@Composable
private fun NodeList(
    overview: ClusterOverview,
    onNode: (NodeOverview) -> Unit,
    onSettings: () -> Unit,
    onNodeAction: (NodeOverview, NodeAction) -> Unit,
    canPower: Boolean,
    canShell: Boolean,
) {
    var sheetFor by remember { mutableStateOf<NodeOverview?>(null) }
    sheetFor?.let { node ->
        NodeActionsSheet(
            node = node,
            canPower = canPower,
            canShell = canShell,
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
        item { UpdateBanner(onClick = onSettings) }
        item { SupportCard() }
        item { Summary(overview.nodes) }
        items(nodes, key = { it.node }) { node ->
            SwipeableNode(node, onLive = { onNodeAction(node, NodeAction.LIVE) }, onMore = { sheetFor = node }) {
                NodeCard(node, onClick = { onNode(node) }, onLongClick = { sheetFor = node })
            }
        }
    }
}

@Composable
private fun Summary(nodes: List<NodeOverview>) {
    val colors = LocalStatusColors.current
    val counts = nodes.groupingBy { it.health }.eachCount()
    Row(Modifier.fillMaxWidth().padding(bottom = 6.dp), horizontalArrangement = Arrangement.spacedBy(20.dp)) {
        SummaryCount(counts[NodeHealth.READY] ?: 0, "ready", colors.ok)
        SummaryCount(counts[NodeHealth.NOT_READY] ?: 0, "not ready", colors.warn)
        SummaryCount(counts[NodeHealth.UNREACHABLE] ?: 0, "unreachable", colors.bad)
    }
}

@Composable
private fun SummaryCount(count: Int, label: String, color: androidx.compose.ui.graphics.Color) {
    Column {
        Text(
            "$count",
            style = MaterialTheme.typography.headlineMedium,
            color = if (count > 0) color else MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Text(label, style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

@Composable
private fun NodeCard(node: NodeOverview, onClick: () -> Unit, onLongClick: () -> Unit) {
    Card(
        Modifier.fillMaxWidth().combinedClickable(
            onClick = { if (node.reachable) onClick() },
            onLongClick = onLongClick,
            onLongClickLabel = "Node actions",
        ),
    ) {
        Column(Modifier.padding(16.dp)) {
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

private fun roleLabel(role: String) = when (role) {
    "controlplane" -> "control plane"
    else -> role
}

/** Shown when the daily check found a newer release; opens Settings → Updates. */
@Composable
private fun UpdateBanner(onClick: () -> Unit) {
    val updates = (LocalContext.current.applicationContext as TalosApp).updateManager
    val state by updates.state.collectAsStateWithLifecycle()
    val available = state as? UpdateState.Available ?: return
    Card(onClick = onClick, modifier = Modifier.fillMaxWidth()) {
        Text(
            "Talosdev Mobile ${available.info.version} is available — tap to update",
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
            Text("Enjoying Talosdev Mobile?", style = MaterialTheme.typography.titleSmall)
            Text(
                "It is free and open source. If it saves you time, you can support its development.",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Row(horizontalArrangement = Arrangement.End, modifier = Modifier.fillMaxWidth()) {
                TextButton(onClick = prompt::never) { Text("Don't ask again") }
                TextButton(onClick = prompt::later) { Text("Not now") }
                TextButton(onClick = {
                    prompt.later()
                    openUrl(context, SPONSOR_URL)
                }) { Text("Sponsor") }
            }
        }
    }
}
