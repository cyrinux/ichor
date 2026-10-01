package name.levis.ichor.ui.kubespan

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
import name.levis.ichor.data.KUBESPAN
import name.levis.ichor.data.OVERVIEW
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.model.ClusterOverview
import name.levis.ichor.model.KubeSpanNode
import name.levis.ichor.model.KubeSpanOverview
import name.levis.ichor.model.KubeSpanPeer
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.DataFreshness
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.formatBytes
import name.levis.ichor.ui.components.localizedDuration

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
                navigationIcon = { IconButton(onClick = onBack) { Icon(Icons.AutoMirrored.Filled.ArrowBack, stringResource(R.string.common_back)) } },
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
    val links = if (down > 0) {
        pluralStringResource(R.plurals.kubespan_links_down, down, down)
    } else {
        stringResource(R.string.kubespan_no_link_down)
    }
    Text(
        pluralStringResource(R.plurals.kubespan_summary, nodes.size, enabled, nodes.size) + " · " + links,
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
                    node.error != null -> StatusPill(stringResource(R.string.common_status_unreachable), colors.bad)
                    !node.enabled -> StatusPill(stringResource(R.string.kubespan_status_off), colors.muted)
                    node.down > 0 -> StatusPill(stringResource(R.string.kubespan_status_down_count, node.down), colors.bad)
                    else -> StatusPill(stringResource(R.string.kubespan_status_up_count, node.up, node.peers.size), colors.ok)
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
                "up" -> StatusPill(stringResource(R.string.kubespan_peer_up), colors.ok)
                "down" -> StatusPill(stringResource(R.string.kubespan_peer_down), colors.bad)
                else -> StatusPill(stringResource(R.string.kubespan_peer_unknown), colors.muted)
            }
        }
        val handshake = peer.lastHandshake.takeIf { it > 0 }?.let {
            stringResource(R.string.kubespan_handshake_ago, localizedDuration(now - it))
        }
        val details = listOfNotNull(
            peer.endpoint.ifBlank { null },
            handshake,
            "↓ ${formatBytes(peer.rx)} ↑ ${formatBytes(peer.tx)}",
        )
        Text(
            details.joinToString("  ·  "),
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}
