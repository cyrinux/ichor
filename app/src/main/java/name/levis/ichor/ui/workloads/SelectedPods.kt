package name.levis.ichor.ui.workloads

import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.compose.viewModel
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.flow.filter
import name.levis.ichor.R
import name.levis.ichor.data.KUBE_WATCH_RETRY_MILLIS
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.data.KubeRepository
import name.levis.ichor.data.StreamItem
import name.levis.ichor.data.isMeteredNetwork
import name.levis.ichor.data.watchForever
import name.levis.ichor.model.KubePage
import name.levis.ichor.model.KubePod
import name.levis.ichor.model.KubeWatchEvent
import name.levis.ichor.model.KubeScope
import name.levis.ichor.model.PodPhaseFilter
import name.levis.ichor.model.PodSelection
import name.levis.ichor.model.SELECTED_PODS_PAGE
import name.levis.ichor.model.applying
import name.levis.ichor.ui.PollWhileStarted
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.cancellableCatching
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.DataFreshness
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.factory

/**
 * The Kubernetes pods of one node or workload ([selection]), page by page in the API server's
 * order: the first page at once, the next ones on scroll (the Linear plan document "U11. Large clusters: home at scale, namespace-first paged lists",
 * Phase 5). Kept in memory only, per phase.
 */
class SelectedPodsViewModel(
    private val talos: TalosRepository,
    kube: KubeRepository,
    metered: () -> Boolean,
    private val selection: PodSelection,
) : PagedListViewModel<KubePod>(kube, metered) {
    private val _phase = MutableStateFlow(PodPhaseFilter.ALL)
    /** The phase the list is narrowed to. */
    val phase: StateFlow<PodPhaseFilter> = _phase.asStateFlow()

    /** The node's Kubernetes name, asked of Talos once. */
    private var kubeNode: String? = null

    override fun key(namespace: String?) = selection.key(_phase.value)

    override fun eagerRows(scope: KubeScope) = SELECTED_PODS_PAGE

    // The first page as full objects (images, containers), the next ones as Table rows (L9, L10).
    override suspend fun page(namespace: String?, token: String): KubePage<KubePod> {
        val table = token.isNotEmpty()
        val phase = _phase.value
        return when (selection) {
            is PodSelection.OnNode -> kube.nodePodsPage(kubeNode(selection.node), phase, token, table)
            is PodSelection.OfWorkload -> kube.workloadPodsPage(selection, phase, token, table)
        }
    }

    override fun detailed(items: List<KubePod>) = items.all { it.images.isNotEmpty() }

    private suspend fun kubeNode(node: String): String = kubeNode ?: talos.kubeNodeName(node).also { kubeNode = it }

    /** Deletions of its pods; a success shows the pod terminating, and soon its replacement. */
    val deletions = PodDeletions(viewModelScope, kube) { refresh() }

    /** Loads the list the first time, else nothing. */
    fun start() = setScope(KubeScope())

    /**
     * Keeps the pods live while called (the screen is visible), a workload's or a node's: the
     * Go core's watch replaces the list, then adds, updates and removes rows as the API server
     * reports them, for the phase chosen. It starts once a load settled, and over again after
     * each one (a refresh, a phase change): the list the watch sends then is newer than the
     * load's, so no change seen meanwhile is lost. A watch that ends (a refusal, the network) is
     * followed again after [KUBE_WATCH_RETRY_MILLIS]; pull-to-refresh stays. A node whose
     * Kubernetes name cannot be found is not followed until the next load.
     */
    suspend fun follow() {
        settled.filter { it > 0 }.collectLatest {
            val phase = _phase.value
            val start: () -> Flow<StreamItem<KubeWatchEvent<KubePod>>> = when (selection) {
                is PodSelection.OfWorkload -> ({ kube.workloadPodsWatch(selection, phase) })
                is PodSelection.OnNode -> {
                    val node = cancellableCatching { kubeNode(selection.node) }.getOrNull() ?: return@collectLatest
                    ({ kube.nodePodsWatch(node, phase) })
                }
            }
            watchForever(start = start) { item ->
                if (item is StreamItem.Item) updateLoaded { it.applying(item.value) { pod -> pod.key } }
            }
        }
    }

    /** Narrows the list to [phase], loading it again from its first page. */
    fun choose(phase: PodPhaseFilter) {
        if (phase == _phase.value) return
        _phase.value = phase
        refresh(reset = true)
    }
}

/** The pods of [selection] with a phase filter, their logs and a delete action (os:admin). */
@Composable
fun SelectedPodsList(
    selection: PodSelection,
    modifier: Modifier = Modifier,
    vm: SelectedPodsViewModel = viewModel(
        key = selection.key(PodPhaseFilter.ALL),
        factory = factory { SelectedPodsViewModel(app.talosRepository, app.kubeRepository, { isMeteredNetwork(app) }, selection) },
    ),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    val progress by vm.progress.collectAsStateWithLifecycle()
    val deleting by vm.deletions.deleting.collectAsStateWithLifecycle()
    val phase by vm.phase.collectAsStateWithLifecycle()
    LaunchedEffect(vm) { vm.start() }
    PollWhileStarted { vm.follow() }
    val actions = remember { PodActionState() }
    PodActionDialogs(actions, vm.deletions)

    Column(modifier.fillMaxSize()) {
        PhaseChips(phase, vm::choose)
        HorizontalDivider()
        val rest = Modifier.weight(1f)
        when (val s = state) {
            UiState.Loading -> LoadingBox(rest)
            is UiState.Failed -> ErrorBox(s.message, vm::refresh, rest)
            is UiState.Loaded -> {
                val load = s.data
                PagedProgress(progress)
                PullToRefreshBox(isRefreshing = s.refreshing, onRefresh = vm::refresh, modifier = rest) {
                    if (load.items.isEmpty()) {
                        EmptyText(stringResource(R.string.pods_empty))
                    } else {
                        val listState = rememberLazyListState()
                        LoadMoreOnScroll(listState, enabled = load.hasMore, loaded = load.items.size, onLoadMore = vm::loadMore)
                        LazyColumn(Modifier.fillMaxSize(), state = listState) {
                            // In the server's order: sorting would move rows as pages arrive.
                            items(load.items, key = { it.key }) { pod ->
                                PodRow(
                                    pod,
                                    showNamespace = selection.showsNamespace,
                                    deleting = pod.key in deleting,
                                    onDelete = { actions.confirm = pod },
                                    onLogs = { actions.logs = pod },
                                    showNode = selection.showsNode,
                                )
                                HorizontalDivider()
                            }
                        }
                    }
                }
                DataFreshness(s, edgeToEdge = false)
            }
        }
    }
}

@Composable
private fun PhaseChips(phase: PodPhaseFilter, onPick: (PodPhaseFilter) -> Unit) {
    Row(
        Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()).padding(horizontal = 16.dp, vertical = 4.dp),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        PodPhaseFilter.entries.forEach { p ->
            FilterChip(selected = p == phase, onClick = { onPick(p) }, label = { Text(stringResource(p.label)) })
        }
    }
}

private val PodPhaseFilter.label: Int
    get() = when (this) {
        PodPhaseFilter.ALL -> R.string.nodes_filter_all
        PodPhaseFilter.RUNNING -> R.string.pod_state_running
        PodPhaseFilter.NOT_COMPLETED -> R.string.pods_phase_not_completed
    }

/** The pods of a workload in a sheet, opened from the Workloads tab or an app's sheet. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun WorkloadPodsSheet(selection: PodSelection.OfWorkload, onDismiss: () -> Unit) {
    ModalBottomSheet(onDismissRequest = onDismiss, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)) {
        Column(Modifier.fillMaxHeight(0.9f)) {
            Column(Modifier.padding(start = 16.dp, end = 16.dp, bottom = 8.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                Text(
                    stringResource(R.string.workload_pods_title, selection.name),
                    style = MaterialTheme.typography.titleLarge,
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                )
                MutedText("${selection.kind}  ·  ${selection.namespace}")
            }
            SelectedPodsList(selection, Modifier.weight(1f))
        }
    }
}
