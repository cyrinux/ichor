package name.levis.ichor.ui.capacity

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.compose.viewModel
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.data.KubeRepository
import name.levis.ichor.model.CapacityFilter
import name.levis.ichor.model.KubeCapacity
import name.levis.ichor.model.KubeObjectRef
import name.levis.ichor.model.NodeLossCandidate
import name.levis.ichor.model.NodeLossPlan
import name.levis.ichor.model.filteredBy
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.cancellableCatching
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.Loaded
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.pageContent
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.uiText

/**
 * The node-loss sheet's state: the node asked for (null: the worst one) and its plan, kept by
 * the ViewModel so a rotation keeps it open. [candidates] and [worst] come from the worst-case
 * plan and stay while another candidate is picked.
 */
data class NodeLossState(
    val node: String?,
    val plan: UiState<NodeLossPlan> = UiState.Loading,
    val candidates: List<NodeLossCandidate> = emptyList(),
    val worst: String = "",
)

/** A capacity read lists every pod of the cluster: loaded on demand, never polled. */
class CapacityViewModel(private val kube: KubeRepository) : LoadingViewModel<KubeCapacity>() {
    override suspend fun fetch() = kube.capacity()

    private val _loss = MutableStateFlow<NodeLossState?>(null)
    val loss: StateFlow<NodeLossState?> = _loss.asStateFlow()
    private var lossJob: Job? = null

    /** Opens the sheet on what losing [node] (null: the worst node) would cost. */
    fun openLoss(node: String?) = load(NodeLossState(node))

    /** Another of the worst case's candidates: the picker stays. */
    fun pickLoss(node: String) = load(_loss.value?.copy(node = node, plan = UiState.Loading) ?: NodeLossState(node))

    private fun load(start: NodeLossState) {
        lossJob?.cancel()
        _loss.value = start
        lossJob = viewModelScope.launch {
            val result = cancellableCatching { kube.nodeLoss(start.node) }
            val plan = result.getOrNull()
            _loss.value = start.copy(
                plan = result.fold(onSuccess = { UiState.Loaded(it) }, onFailure = { UiState.Failed(it.uiText()) }),
                candidates = if (plan?.worstCase == true) plan.candidates else start.candidates,
                worst = if (plan?.worstCase == true) plan.node else start.worst,
            )
        }
    }

    fun closeLoss() {
        lossJob?.cancel()
        _loss.value = null
    }
}

/**
 * Capacity (os:admin): how much is free per node and for the cluster, whether one node's pods
 * would fit on the others, and each workload with what decides who goes first (priority, QoS
 * class, memory limit). A node, or "What if a node goes down", opens what losing it costs;
 * a suggestion there opens the object's scale dialog ([onScale]), nothing is scaled here.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun CapacityScreen(
    onBack: () -> Unit,
    onScale: (KubeObjectRef) -> Unit,
    vm: CapacityViewModel = viewModel(factory = factory { CapacityViewModel(app.kubeRepository) }),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    val loss by vm.loss.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { if (state == UiState.Loading) vm.refresh() }

    loss?.let { l ->
        NodeLossSheet(
            l,
            onNode = vm::pickLoss,
            onRetry = { vm.pickLoss(l.node.orEmpty()) },
            onScale = { ref ->
                vm.closeLoss()
                onScale(ref)
            },
            onDismiss = vm::closeLoss,
        )
    }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text(stringResource(R.string.capacity_title)) },
                navigationIcon = { BackButton(onBack) },
                actions = { TooltipIconButton(Icons.Outlined.Refresh, stringResource(R.string.common_refresh), onClick = { vm.refresh() }) },
            )
        },
    ) { padding ->
        Loaded(state, vm::refresh, Modifier.pageContent(padding)) { capacity ->
            CapacityList(capacity, onLoss = vm::openLoss)
        }
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun CapacityList(capacity: KubeCapacity, onLoss: (node: String?) -> Unit) {
    var filter by rememberSaveable { mutableStateOf(CapacityFilter.ALL) }
    val workloads = remember(capacity, filter) { capacity.workloads.filteredBy(filter) }

    LazyColumn(
        Modifier.fillMaxSize(),
        contentPadding = PaddingValues(horizontal = 16.dp, vertical = 8.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        item(key = "header") { CapacityHeader(capacity, onWhatIf = { onLoss(null) }) }
        item(key = "nodes-title") { Text(stringResource(R.string.capacity_nodes_title), style = MaterialTheme.typography.titleSmall) }
        items(capacity.nodes, key = { "node:${it.name}" }) { n -> CapacityNodeRow(n, capacity.metricsAvailable, onOpen = { onLoss(n.name) }) }
        item(key = "workloads-title") {
            Text(stringResource(R.string.capacity_workloads_title), style = MaterialTheme.typography.titleSmall)
            FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                CapacityFilter.entries.forEach { f ->
                    FilterChip(selected = filter == f, onClick = { filter = f }, label = { Text(filterLabel(f)) })
                }
            }
        }
        if (workloads.isEmpty()) item(key = "workloads-empty") { MutedText(stringResource(R.string.capacity_workloads_empty)) }
        items(workloads, key = { it.key }) { w -> CapacityWorkloadRow(w) }
    }
}

@Composable
private fun filterLabel(f: CapacityFilter): String = when (f) {
    CapacityFilter.ALL -> stringResource(R.string.capacity_filter_all)
    CapacityFilter.AT_RISK -> stringResource(R.string.capacity_filter_at_risk)
    CapacityFilter.NO_PRIORITY_CLASS -> stringResource(R.string.capacity_no_class)
}
