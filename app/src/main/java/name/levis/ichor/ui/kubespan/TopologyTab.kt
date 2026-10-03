package name.levis.ichor.ui.kubespan

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.data.TOPOLOGY
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.model.ClusterTopology
import name.levis.ichor.model.TopologyLink
import name.levis.ichor.model.TopologyLinkSide
import name.levis.ichor.model.TopologyNode
import name.levis.ichor.model.isBroken
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.components.localizedDuration
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.formatBytes

class TopologyViewModel(private val talos: TalosRepository) : LoadingViewModel<ClusterTopology>() {
    override val keepsDataOnFailure = true
    override fun cached(): TalosRepository.Timed<ClusterTopology>? = talos.cached(TOPOLOGY)
    override val restores get() = talos.restores
    override suspend fun fetch() = talos.topology()
}

/** The map tab: a one-line summary, the map, its legend, and a sheet for a tapped link. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun TopologyContent(topology: ClusterTopology, onNode: (TopologyNode) -> Unit) {
    var link by remember { mutableStateOf<TopologyLink?>(null) }
    val names = topology.nodes.associate { it.id to it.hostname.ifBlank { it.id } }

    LazyColumn(contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        item { TopologySummary(topology) }
        if (topology.nodes.isEmpty()) {
            item { Text(stringResource(R.string.topology_empty), style = MaterialTheme.typography.bodyMedium) }
        } else {
            item { TopologyMap(topology, onNode = onNode, onLink = { link = it }) }
            item { TopologyLegend() }
            if (topology.nodes.any { !it.queried }) {
                item {
                    Text(
                        stringResource(R.string.topology_not_queried),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
        }
    }

    link?.let { shown ->
        ModalBottomSheet(onDismissRequest = { link = null }) {
            LinkDetails(shown, names, Modifier.padding(start = 16.dp, end = 16.dp, bottom = 32.dp))
        }
    }
}

@Composable
private fun TopologySummary(topology: ClusterTopology) {
    val broken = topology.links.count { it.isBroken }
    val parts = listOf(
        pluralStringResource(R.plurals.topology_sites, topology.sites.size, topology.sites.size),
        pluralStringResource(R.plurals.topology_links, topology.links.size, topology.links.size),
        if (broken > 0) pluralStringResource(R.plurals.kubespan_links_down, broken, broken) else stringResource(R.string.kubespan_no_link_down),
    )
    Text(
        parts.joinToString(" · "),
        style = MaterialTheme.typography.bodyMedium,
        color = if (broken > 0) LocalStatusColors.current.bad else MaterialTheme.colorScheme.onSurfaceVariant,
    )
}

@Composable
private fun LinkDetails(link: TopologyLink, names: Map<String, String>, modifier: Modifier = Modifier) {
    Column(modifier, verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Text("${names[link.a] ?: link.a}  ↔  ${names[link.b] ?: link.b}", style = MaterialTheme.typography.titleMedium)
        link.sides.forEachIndexed { i, side ->
            if (i > 0) HorizontalDivider()
            LinkSideRow(side, names)
        }
        if (link.sides.size < 2) {
            Text(
                stringResource(R.string.topology_link_one_side),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

@Composable
private fun LinkSideRow(side: TopologyLinkSide, names: Map<String, String>) {
    val colors = LocalStatusColors.current
    val now = System.currentTimeMillis() / 1000
    Column(Modifier.fillMaxWidth()) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                stringResource(R.string.topology_link_seen_from, names[side.from] ?: side.from),
                style = MaterialTheme.typography.bodyMedium,
                modifier = Modifier.weight(1f),
            )
            when (side.state) {
                "up" -> StatusPill(stringResource(R.string.kubespan_peer_up), colors.ok)
                "down" -> StatusPill(stringResource(R.string.kubespan_peer_down), colors.bad)
                else -> StatusPill(stringResource(R.string.kubespan_peer_unknown), colors.muted)
            }
        }
        if (side.endpoint.isNotBlank()) {
            Text(side.endpoint, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace)
        }
        val handshake = side.lastHandshake.takeIf { it > 0 }?.let {
            stringResource(R.string.kubespan_handshake_ago, localizedDuration(now - it))
        }
        Text(
            listOfNotNull(handshake, "↓ ${formatBytes(side.rx)} ↑ ${formatBytes(side.tx)}").joinToString("  ·  "),
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}
