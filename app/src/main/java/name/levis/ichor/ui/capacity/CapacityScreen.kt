package name.levis.ichor.ui.capacity

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.text.TextAutoSize
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import name.levis.ichor.ui.design.IchorScaffold
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Text
import name.levis.ichor.ui.design.IchorTopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
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
import name.levis.ichor.model.majority
import name.levis.ichor.model.overcommittedFirst
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.cancellableCatching
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.JumpSection
import name.levis.ichor.ui.components.Loaded
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionJumpRail
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.pageContent
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.uiText

/**
 * The node-loss sheet's state: the nodes asked for (empty: the worst one) and their plan, kept
 * by the ViewModel so a rotation keeps it open. [candidates] and [worst] come from the
 * worst-case plan and stay while other nodes are picked.
 */
data class NodeLossState(
    val selected: Set<String> = emptySet(),
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
    fun openLoss(node: String?) = load(NodeLossState(selected = node?.let(::setOf) ?: emptySet()))

    /** Toggles [node] in the selection: one node simulates at once, several wait for the button. */
    fun toggleLoss(node: String) {
        val current = _loss.value ?: NodeLossState()
        val selected = if (node in current.selected) current.selected - node else current.selected + node

        when (selected.size) {
            0, 1 -> load(current.copy(selected = selected, plan = UiState.Loading))
            else -> _loss.value = current.copy(selected = selected)
        }
    }

    /** Simulates every selected node at once. */
    fun simulateLoss() {
        _loss.value?.let { load(it.copy(plan = UiState.Loading)) }
    }

    private fun load(start: NodeLossState) {
        lossJob?.cancel()
        _loss.value = start
        lossJob = viewModelScope.launch {
            val result = cancellableCatching { kube.nodeLoss(start.selected.toList()) }
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
            onToggle = vm::toggleLoss,
            onSimulate = vm::simulateLoss,
            onRetry = vm::simulateLoss,
            onScale = { ref ->
                vm.closeLoss()
                onScale(ref)
            },
            onDismiss = vm::closeLoss,
        )
    }

    IchorScaffold(
        topBar = {
            IchorTopAppBar(
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

@Composable
private fun CapacityList(capacity: KubeCapacity, onLoss: (node: String?) -> Unit) {
    var filter by rememberSaveable { mutableStateOf(CapacityFilter.ALL) }
    val workloads = remember(capacity, filter) { capacity.workloads.filteredBy(filter) }
    // Hidden values come from the whole list, so a filter does not change what a row shows.
    val majority = remember(capacity) { capacity.workloads.majority() }
    val nodes = remember(capacity) { capacity.nodes.overcommittedFirst() }
    val listState = rememberLazyListState()
    val summaryLabel = stringResource(R.string.capacity_section_summary)
    val nodesLabel = stringResource(R.string.capacity_nodes_title)
    val workloadsLabel = stringResource(R.string.capacity_section_workloads)
    // Header positions follow the items below: header, nodes title, one row per node, workloads title.
    val sections = remember(capacity.nodes.size, summaryLabel, nodesLabel, workloadsLabel) {
        listOf(
            JumpSection("header", summaryLabel, 0),
            JumpSection("nodes-title", nodesLabel, 1),
            JumpSection("workloads-title", workloadsLabel, 2 + capacity.nodes.size),
        )
    }

    Box(Modifier.fillMaxSize()) {
        LazyColumn(
            Modifier.fillMaxSize(),
            state = listState,
            contentPadding = PaddingValues(horizontal = 16.dp, vertical = 8.dp),
            verticalArrangement = Arrangement.spacedBy(4.dp),
        ) {
            item(key = "header") { CapacityHeader(capacity, onWhatIf = { onLoss(null) }) }
            item(key = "nodes-title") { SectionTitle(stringResource(R.string.capacity_nodes_title)) }
            items(nodes, key = { "node:${it.name}" }) { n -> CapacityNodeRow(n, capacity.metricsAvailable, onOpen = { onLoss(n.name) }) }
            item(key = "workloads-title") {
                Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
                    SectionTitle(stringResource(R.string.capacity_workloads_title))
                    WorkloadFilter(filter, onSelect = { filter = it })
                }
            }
            if (workloads.isEmpty()) item(key = "workloads-empty") { MutedText(stringResource(R.string.capacity_workloads_empty)) }
            items(workloads, key = { it.key }) { w -> CapacityWorkloadRow(w, majority) }
        }
        SectionJumpRail(sections, listState, Modifier.align(Alignment.CenterEnd))
    }
}

/** "All", "At risk", "No priority class" as one segmented row. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun WorkloadFilter(filter: CapacityFilter, onSelect: (CapacityFilter) -> Unit) {
    val entries = CapacityFilter.entries
    SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth()) {
        entries.forEachIndexed { i, f ->
            SegmentedButton(
                selected = filter == f,
                onClick = { onSelect(f) },
                shape = SegmentedButtonDefaults.itemShape(i, entries.size),
                icon = {},
            ) {
                Text(
                    filterLabel(f),
                    maxLines = 1,
                    softWrap = false,
                    autoSize = TextAutoSize.StepBased(minFontSize = 10.sp, maxFontSize = MaterialTheme.typography.labelLarge.fontSize),
                )
            }
        }
    }
}

@Composable
private fun filterLabel(f: CapacityFilter): String = when (f) {
    CapacityFilter.ALL -> stringResource(R.string.capacity_filter_all)
    CapacityFilter.AT_RISK -> stringResource(R.string.capacity_filter_at_risk)
    CapacityFilter.NO_PRIORITY_CLASS -> stringResource(R.string.capacity_no_class)
}
