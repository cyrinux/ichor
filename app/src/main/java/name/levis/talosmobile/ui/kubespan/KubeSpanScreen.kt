package name.levis.talosmobile.ui.kubespan

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
import androidx.compose.material3.HorizontalDivider
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
import name.levis.talosmobile.data.KUBESPAN
import name.levis.talosmobile.data.OVERVIEW
import name.levis.talosmobile.data.TalosRepository
import name.levis.talosmobile.model.ClusterOverview
import name.levis.talosmobile.model.KubeSpanNode
import name.levis.talosmobile.model.KubeSpanOverview
import name.levis.talosmobile.model.KubeSpanPeer
import name.levis.talosmobile.ui.LoadingViewModel
import name.levis.talosmobile.ui.UiState
import name.levis.talosmobile.ui.app
import name.levis.talosmobile.ui.components.DataFreshness
import name.levis.talosmobile.ui.components.ErrorBox
import name.levis.talosmobile.ui.components.LoadingBox
import name.levis.talosmobile.ui.components.StatusPill
import name.levis.talosmobile.ui.factory
import name.levis.talosmobile.ui.theme.LocalStatusColors
import name.levis.talosmobile.util.formatBytes
import name.levis.talosmobile.util.formatDuration

class KubeSpanViewModel(private val talos: TalosRepository) : LoadingViewModel<KubeSpanOverview>() {
    override fun cached(): TalosRepository.Timed<KubeSpanOverview>? = talos.cached(KUBESPAN)
    override suspend fun fetch() = talos.kubespan()

    /** node address -> hostname, from the cached overview when available. */
    fun hostnames(): Map<String, String> =
        talos.cached<ClusterOverview>(OVERVIEW)?.value?.nodes?.associate { it.node to it.hostname }.orEmpty()
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun KubeSpanScreen(
    onBack: () -> Unit,
    vm: KubeSpanViewModel = viewModel(factory = factory { KubeSpanViewModel(app.talosRepository) }),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { if (state == UiState.Loading) vm.refresh() }

    Scaffold(
        bottomBar = { DataFreshness(state) },
        topBar = {
            TopAppBar(
                title = { Text("KubeSpan") },
                navigationIcon = { IconButton(onClick = onBack) { Icon(Icons.AutoMirrored.Filled.ArrowBack, "Back") } },
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
                val hostnames = vm.hostnames()
                LazyColumn(contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                    item { Summary(s.data.nodes) }
                    items(s.data.nodes, key = { it.node }) { NodeCard(it, hostnames[it.node] ?: it.node) }
                }
            }
        }
    }
}

@Composable
private fun Summary(nodes: List<KubeSpanNode>) {
    val enabled = nodes.count { it.enabled }
    val down = nodes.sumOf { it.down }
    Text(
        "KubeSpan on $enabled of ${nodes.size} nodes" + if (down > 0) " · $down peer link(s) down" else " · no link down",
        style = MaterialTheme.typography.bodyMedium,
        color = if (down > 0) LocalStatusColors.current.bad else MaterialTheme.colorScheme.onSurfaceVariant,
    )
}

@Composable
private fun NodeCard(node: KubeSpanNode, hostname: String) {
    val colors = LocalStatusColors.current
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) {
                    Text(hostname, style = MaterialTheme.typography.titleMedium)
                    Text(node.node, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace)
                }
                when {
                    node.error != null -> StatusPill("Unreachable", colors.bad)
                    !node.enabled -> StatusPill("Off", colors.muted)
                    node.down > 0 -> StatusPill("${node.down} down", colors.bad)
                    else -> StatusPill("${node.up}/${node.peers.size} up", colors.ok)
                }
            }
            node.error?.let { Text(it, style = MaterialTheme.typography.bodySmall, color = colors.bad) }
            node.peers.forEachIndexed { i, peer ->
                if (i > 0) HorizontalDivider()
                PeerRow(peer)
            }
        }
    }
}

@Composable
private fun PeerRow(peer: KubeSpanPeer) {
    val colors = LocalStatusColors.current
    val now = System.currentTimeMillis() / 1000
    Column(Modifier.padding(vertical = 4.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(peer.label.ifBlank { peer.publicKey.take(12) }, style = MaterialTheme.typography.bodyMedium, modifier = Modifier.weight(1f))
            when (peer.state) {
                "up" -> StatusPill("Up", colors.ok)
                "down" -> StatusPill("Down", colors.bad)
                else -> StatusPill("Unknown", colors.muted)
            }
        }
        val details = listOfNotNull(
            peer.endpoint.ifBlank { null },
            peer.lastHandshake.takeIf { it > 0 }?.let { "handshake ${formatDuration(now - it)} ago" },
            "↓ ${formatBytes(peer.rx)} ↑ ${formatBytes(peer.tx)}",
        )
        Text(
            details.joinToString("  ·  "),
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}
