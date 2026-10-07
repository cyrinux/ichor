package name.levis.ichor.ui.nodes

import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import name.levis.ichor.R
import name.levis.ichor.data.TOPOLOGY
import name.levis.ichor.data.activeSummary
import name.levis.ichor.model.ClusterTopology
import name.levis.ichor.model.Feature
import name.levis.ichor.model.NodeFilter
import name.levis.ichor.model.NodeGroup
import name.levis.ichor.model.NodeOverview
import name.levis.ichor.model.allows
import name.levis.ichor.model.filterNodes
import name.levis.ichor.model.groupNodes
import name.levis.ichor.model.healthCounts
import name.levis.ichor.model.key
import name.levis.ichor.model.sharedVersion
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.DataFreshness
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.SearchField
import name.levis.ichor.ui.components.emptyOrNoMatch
import name.levis.ichor.ui.overview.NodeAction
import name.levis.ichor.ui.overview.NodeActionsSheet
import name.levis.ichor.ui.overview.OverviewViewModel
import name.levis.ichor.ui.overview.SwipeableNodeRow
import name.levis.ichor.ui.overview.healthSummary
import name.levis.ichor.ui.overview.rememberPublicIpDetection
import name.levis.ichor.ui.overview.rememberWakeOnLan
import name.levis.ichor.ui.overview.siteLabel
import name.levis.ichor.ui.components.pageContent

/**
 * Every node of a large cluster, where the dense home card only shows dots: search by hostname
 * or address, filter by health and site, the same rows and actions as home. [vm] is the
 * overview's, so both show the same load and a pull here refreshes home too.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun NodesScreen(
    initialFilter: NodeFilter?,
    vm: OverviewViewModel,
    onBack: () -> Unit,
    onNode: (NodeOverview) -> Unit,
    onNodeAction: (NodeOverview, NodeAction) -> Unit,
) {
    val state by vm.state.collectAsStateWithLifecycle()
    val config by vm.configs.config.collectAsStateWithLifecycle()
    val summary = config?.activeSummary
    var query by rememberSaveable { mutableStateOf("") }
    var filter by rememberSaveable { mutableStateOf(initialFilter) }
    var site by rememberSaveable { mutableStateOf<String?>(null) }
    var sheetFor by remember { mutableStateOf<NodeOverview?>(null) }
    val wakeOnLan = rememberWakeOnLan(summary?.fingerprint)
    // Shows what home found; finding them is only offered there.
    val publicIps = rememberPublicIpDetection(summary?.fingerprint, canDetect = false)
    sheetFor?.let { node ->
        NodeActionsSheet(
            node = node,
            canPower = summary?.allows(Feature.POWER) == true,
            canShell = summary?.allows(Feature.DEBUG_SHELL) == true,
            wol = wakeOnLan(node),
            onAction = { onNodeAction(node, it) },
            onDismiss = { sheetFor = null },
        )
    }

    Scaffold(
        bottomBar = { DataFreshness(state) },
        topBar = {
            TopAppBar(
                title = { Text(stringResource(R.string.overview_stat_nodes)) },
                navigationIcon = { BackButton(onBack) },
            )
        },
    ) { padding ->
        when (val s = state) {
            UiState.Loading -> LoadingBox(Modifier.pageContent(padding))
            is UiState.Failed -> ErrorBox(s.message, vm::refresh, Modifier.pageContent(padding))
            is UiState.Loaded -> Column(Modifier.pageContent(padding).fillMaxSize()) {
                // In the map's order, site by site, as on home.
                val groups = remember(s.data) { vm.talos.cached<ClusterTopology>(TOPOLOGY)?.value.groupNodes(s.data.nodes) }
                val shown = remember(groups, query, filter, site) { groups.filterNodes(query, filter, site) }
                val sharedVersion = remember(s.data) { s.data.nodes.sharedVersion() }
                val counts = remember(s.data) { s.data.nodes.healthCounts() }
                Column(Modifier.padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                    Text(healthSummary(counts), style = MaterialTheme.typography.labelLarge)
                    SearchField(query, { query = it }, stringResource(R.string.nodes_search), Modifier.fillMaxWidth())
                    FilterChips(filter) { filter = it }
                    // A site filter only says something with more than one.
                    if (groups.size > 1) SiteChips(groups, site) { site = it }
                }
                HorizontalDivider()
                PullToRefreshBox(isRefreshing = s.refreshing, onRefresh = vm::refresh, modifier = Modifier.weight(1f)) {
                    if (shown.isEmpty()) {
                        EmptyText(emptyOrNoMatch(query, R.string.nodes_none, R.string.nodes_no_match))
                    } else {
                        NodeList(shown, sites = groups.size > 1) { node ->
                            SwipeableNodeRow(
                                node,
                                publicIps,
                                sharedVersion,
                                onNode = onNode,
                                onLive = { onNodeAction(it, NodeAction.LIVE) },
                                onMore = { sheetFor = it },
                                background = MaterialTheme.colorScheme.surface,
                            )
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun NodeList(groups: List<NodeGroup>, sites: Boolean, row: @Composable (NodeOverview) -> Unit) {
    LazyColumn(Modifier.fillMaxSize()) {
        groups.forEach { group ->
            if (sites) item(key = "site:${group.key}") {
                Text(
                    siteLabel(group),
                    style = MaterialTheme.typography.labelLarge,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.padding(start = 16.dp, end = 16.dp, top = 12.dp, bottom = 4.dp),
                )
            }
            items(group.nodes, key = { "node:${it.node}" }) { node ->
                row(node)
                HorizontalDivider()
            }
        }
    }
}

/** All, needing attention, or one health. */
@Composable
private fun FilterChips(current: NodeFilter?, onSelect: (NodeFilter?) -> Unit) {
    Row(Modifier.horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        FilterChip(selected = current == null, onClick = { onSelect(null) }, label = { Text(stringResource(R.string.nodes_filter_all)) })
        NodeFilter.entries.forEach { filter ->
            FilterChip(selected = current == filter, onClick = { onSelect(filter) }, label = { Text(stringResource(filterLabel(filter))) })
        }
    }
}

private fun filterLabel(filter: NodeFilter) = when (filter) {
    NodeFilter.ATTENTION -> R.string.nodes_filter_attention
    NodeFilter.READY -> R.string.common_status_ready
    NodeFilter.NOT_READY -> R.string.common_status_not_ready
    NodeFilter.UNREACHABLE -> R.string.common_status_unreachable
}

/** All sites, or one ([NodeGroup.key]). */
@Composable
private fun SiteChips(groups: List<NodeGroup>, current: String?, onSelect: (String?) -> Unit) {
    Row(Modifier.horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        FilterChip(selected = current == null, onClick = { onSelect(null) }, label = { Text(stringResource(R.string.nodes_site_all)) })
        groups.forEach { group ->
            FilterChip(
                selected = current == group.key,
                onClick = { onSelect(group.key) },
                label = { Text(siteLabel(group), maxLines = 1, overflow = TextOverflow.Ellipsis) },
            )
        }
    }
}
