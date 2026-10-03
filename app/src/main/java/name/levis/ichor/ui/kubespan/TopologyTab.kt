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
import androidx.compose.material3.OutlinedButton
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
import name.levis.ichor.model.NETPERF_LATENCY
import name.levis.ichor.model.NETPERF_PATH_HOST
import name.levis.ichor.model.NETPERF_PATH_POD
import name.levis.ichor.model.NETPERF_THROUGHPUT
import name.levis.ichor.model.NetPerfReport
import name.levis.ichor.model.formatMbps
import name.levis.ichor.model.formatMicros
import name.levis.ichor.model.latestBetween
import name.levis.ichor.model.pickNode
import name.levis.ichor.model.podThroughputMbps
import name.levis.ichor.ui.workloads.netPerfViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import java.text.DateFormat
import java.util.Date
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

/**
 * The map tab: a one-line summary, the map, its legend, and a sheet for a tapped link. Two
 * nodes picked on the map, or a link, open the network test between them.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun TopologyContent(topology: ClusterTopology, onNode: (TopologyNode) -> Unit) {
    var link by remember { mutableStateOf<TopologyLink?>(null) }
    val names = topology.nodes.associate { it.id to it.hostname.ifBlank { it.id } }
    // Node names of the network test are Kubernetes node names: the hostnames on Talos.
    val netPerf = netPerfViewModel()
    val history by netPerf.history.collectAsStateWithLifecycle()
    val session by netPerf.session.collectAsStateWithLifecycle()
    var picking by remember { mutableStateOf(false) }
    var picked by remember { mutableStateOf(emptyList<String>()) }
    var testing by remember { mutableStateOf(false) }
    fun test(client: String, server: String) {
        netPerf.prepare(client, server)
        picking = false
        picked = emptyList()
        link = null
        testing = true
    }
    val tests = remember(topology, history) {
        topology.links.mapNotNull { l -> history.latestBetween(l.a, l.b)?.let { l to it } }.toMap()
    }

    LazyColumn(contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        item { TopologySummary(topology) }
        if (topology.nodes.isNotEmpty()) {
            item {
                NetPerfPickBar(
                    picking, picked.size, running = session?.running == true,
                    onToggle = {
                        picking = !picking
                        picked = emptyList()
                    },
                    onOpen = { testing = true },
                )
            }
        }
        if (topology.nodes.isEmpty()) {
            item { Text(stringResource(R.string.topology_empty), style = MaterialTheme.typography.bodyMedium) }
        } else {
            item {
                TopologyMap(
                    topology,
                    onNode = { node ->
                        if (picking) {
                            picked = picked.pickNode(node.id)
                            if (picked.size == 2) test(client = picked[0], server = picked[1])
                        } else {
                            onNode(node)
                        }
                    },
                    onLink = { link = it },
                    speeds = tests.mapValues { (_, test) -> formatMbps(test.podThroughputMbps ?: 0.0) },
                    picked = picked,
                )
            }
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
            LinkDetails(
                shown, names, tests[shown],
                onTest = { test(client = shown.a, server = shown.b) }.takeUnless { session?.running == true },
                modifier = Modifier.padding(start = 16.dp, end = 16.dp, bottom = 32.dp),
            )
        }
    }

    if (testing) NetPerfSheet(netPerf, onDismiss = { testing = false })
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
private fun LinkDetails(
    link: TopologyLink,
    names: Map<String, String>,
    test: NetPerfReport?,
    /** Opens the network test over this link; null while one runs. */
    onTest: (() -> Unit)?,
    modifier: Modifier = Modifier,
) {
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
        test?.let {
            HorizontalDivider()
            LinkTest(it)
        }
        OutlinedButton(onClick = { onTest?.invoke() }, enabled = onTest != null, modifier = Modifier.fillMaxWidth()) {
            Text(stringResource(R.string.topology_link_test_run))
        }
    }
}

/** The last network test between the link's two nodes: direction, figures and when it ran. */
@Composable
private fun LinkTest(test: NetPerfReport) {
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    val pod = test.results.filter { it.path == NETPERF_PATH_POD && it.error.isEmpty() }
    val host = test.results.firstOrNull { it.path == NETPERF_PATH_HOST && it.test == NETPERF_THROUGHPUT && it.error.isEmpty() }
    Column(Modifier.fillMaxWidth()) {
        Text(stringResource(R.string.topology_link_test), style = MaterialTheme.typography.bodyMedium)
        Text("${test.client} → ${test.server}", style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace)
        Text(
            listOfNotNull(
                test.podThroughputMbps?.let(::formatMbps),
                pod.firstOrNull { it.test == NETPERF_LATENCY }?.latency?.let { "p50 " + formatMicros(it.p50) },
                host?.let { stringResource(R.string.topology_link_test_host, formatMbps(it.throughputMbps)) },
            ).joinToString("  ·  "),
            style = MaterialTheme.typography.bodySmall,
        )
        Text(
            DateFormat.getDateTimeInstance(DateFormat.SHORT, DateFormat.SHORT).format(Date(test.started)),
            style = MaterialTheme.typography.bodySmall,
            color = muted,
        )
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
