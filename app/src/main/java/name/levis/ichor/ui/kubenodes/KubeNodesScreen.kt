package name.levis.ichor.ui.kubenodes

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
import name.levis.ichor.model.KUBE_NODE_FILTERS
import name.levis.ichor.model.KubeNodeInfo
import name.levis.ichor.model.KubeStatusGroup
import name.levis.ichor.model.NodeFilter
import name.levis.ichor.model.byStatus
import name.levis.ichor.model.filterKubeNodes
import name.levis.ichor.model.kubeHealthCounts
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.DataFreshness
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.SearchField
import name.levis.ichor.ui.components.emptyOrNoMatch
import name.levis.ichor.ui.components.pageContent
import name.levis.ichor.ui.overview.KubeHomeViewModel
import name.levis.ichor.ui.overview.KubeNodeRow
import name.levis.ichor.ui.overview.healthSummary
import name.levis.ichor.ui.overview.statusLabel

/**
 * Every node of a large cluster added from a kubeconfig, where the dense home card only
 * shows dots: search by name or address, filter by status, the nodes grouped by status (the
 * worst first), the same rows as home; a row opens the node's screen. [vm] is the Kubernetes
 * home's, so both show the same load and a pull here refreshes home too.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun KubeNodesScreen(
    initialFilter: NodeFilter?,
    vm: KubeHomeViewModel,
    onBack: () -> Unit,
    onNode: (name: String) -> Unit,
) {
    val state by vm.state.collectAsStateWithLifecycle()
    var query by rememberSaveable { mutableStateOf("") }
    var filter by rememberSaveable { mutableStateOf(initialFilter) }

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
                val nodes = s.data.nodes
                val groups = remember(nodes, query, filter) { nodes.filterKubeNodes(query, filter).byStatus() }
                val counts = remember(nodes) { nodes.kubeHealthCounts() }
                Column(Modifier.padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                    Text(healthSummary(counts), style = MaterialTheme.typography.labelLarge)
                    SearchField(query, { query = it }, stringResource(R.string.nodes_search), Modifier.fillMaxWidth())
                    FilterChips(filter) { filter = it }
                }
                HorizontalDivider()
                PullToRefreshBox(isRefreshing = s.refreshing, onRefresh = vm::refresh, modifier = Modifier.weight(1f)) {
                    if (groups.isEmpty()) {
                        EmptyText(emptyOrNoMatch(query, R.string.nodes_none, R.string.nodes_no_match))
                    } else {
                        NodeList(groups) { node ->
                            KubeNodeRow(node, onClick = { onNode(node.name) }, modifier = Modifier.padding(horizontal = 16.dp, vertical = 10.dp))
                        }
                    }
                }
            }
        }
    }
}

/** The nodes under a header per status, so a scroll lands on the problems first. */
@Composable
private fun NodeList(groups: List<KubeStatusGroup>, row: @Composable (KubeNodeInfo) -> Unit) {
    LazyColumn(Modifier.fillMaxSize()) {
        groups.forEach { group ->
            item(key = "status:${group.status}") {
                Text(
                    "${statusLabel(group.status)}  ·  ${group.nodes.size}",
                    style = MaterialTheme.typography.labelLarge,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.padding(start = 16.dp, end = 16.dp, top = 12.dp, bottom = 4.dp),
                )
            }
            items(group.nodes, key = { "node:${it.name}" }) { node ->
                row(node)
                HorizontalDivider()
            }
        }
    }
}

/** All, needing attention, or one status: Kubernetes never says unreachable. */
@Composable
private fun FilterChips(current: NodeFilter?, onSelect: (NodeFilter?) -> Unit) {
    Row(Modifier.horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        FilterChip(selected = current == null, onClick = { onSelect(null) }, label = { Text(stringResource(R.string.nodes_filter_all)) })
        KUBE_NODE_FILTERS.forEach { filter ->
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
