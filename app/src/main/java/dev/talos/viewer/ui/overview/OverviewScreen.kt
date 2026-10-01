package dev.talos.viewer.ui.overview

import androidx.compose.foundation.clickable
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
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import dev.talos.viewer.data.ConfigRepository
import dev.talos.viewer.data.TalosRepository
import dev.talos.viewer.model.ClusterOverview
import dev.talos.viewer.model.NodeHealth
import dev.talos.viewer.model.NodeOverview
import dev.talos.viewer.model.health
import dev.talos.viewer.ui.LoadingViewModel
import dev.talos.viewer.ui.UiState
import dev.talos.viewer.ui.app
import dev.talos.viewer.ui.components.ErrorBox
import dev.talos.viewer.ui.components.LoadingBox
import dev.talos.viewer.ui.components.NodeHealthPill
import dev.talos.viewer.ui.factory
import dev.talos.viewer.ui.theme.LocalStatusColors

class OverviewViewModel(
    private val talos: TalosRepository,
    val configs: ConfigRepository,
) : LoadingViewModel<ClusterOverview>() {
    override suspend fun fetch() = talos.overview()
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun OverviewScreen(
    onNode: (NodeOverview) -> Unit,
    onEtcd: () -> Unit,
    onHealth: () -> Unit,
    onSettings: () -> Unit,
    vm: OverviewViewModel = viewModel(factory = factory { OverviewViewModel(app.talosRepository, app.configRepository) }),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    val config by vm.configs.config.collectAsStateWithLifecycle()

    // Reload whenever the active context changes (including first composition).
    LaunchedEffect(config?.activeContext) { vm.refresh() }

    Scaffold(
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text("Cluster")
                        config?.activeContext?.let {
                            Text(it, style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.primary)
                        }
                    }
                },
                actions = {
                    IconButton(onClick = onHealth) { Icon(Icons.Outlined.Favorite, "Cluster health") }
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
                NodeList(s.data, onNode)
            }
        }
    }
}

@Composable
private fun NodeList(overview: ClusterOverview, onNode: (NodeOverview) -> Unit) {
    val nodes = overview.nodes.sortedWith(compareBy({ it.role != "controlplane" }, { it.hostname }))
    LazyColumn(
        contentPadding = PaddingValues(16.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
        modifier = Modifier.fillMaxSize(),
    ) {
        item { Summary(overview.nodes) }
        items(nodes, key = { it.node }) { node ->
            NodeCard(node, onClick = { onNode(node) })
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
private fun NodeCard(node: NodeOverview, onClick: () -> Unit) {
    Card(Modifier.fillMaxWidth().clickable(enabled = node.reachable, onClick = onClick)) {
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
