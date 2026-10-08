package name.levis.ichor.ui.events

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import name.levis.ichor.R
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.model.KubeEventList
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.DataFreshness
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.pageContent
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.workloads.KubeEventRow

/** The cluster's Kubernetes events, the Warning ones by default; loaded on demand, never polled. */
class KubeEventsViewModel(private val talos: TalosRepository) : LoadingViewModel<KubeEventList>() {
    private val _warningsOnly = MutableStateFlow(true)
    /** Whether the Normal events are left out. */
    val warningsOnly: StateFlow<Boolean> = _warningsOnly.asStateFlow()

    override suspend fun fetch() = talos.kubeClusterEvents(_warningsOnly.value)

    /** Shows the Warning events only, or every event, loading the list again. */
    fun choose(warningsOnly: Boolean) {
        if (warningsOnly == _warningsOnly.value) return
        _warningsOnly.value = warningsOnly
        refresh(reset = true)
    }
}

/**
 * The events of every namespace and of the nodes, newest first, like `kubectl get events -A`:
 * the Kubernetes counterpart of the Talos events, for a cluster added from a kubeconfig. The
 * Warning ones by default; Kubernetes keeps events for an hour.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun KubeEventsScreen(
    onBack: () -> Unit,
    vm: KubeEventsViewModel = viewModel(factory = factory { KubeEventsViewModel(app.talosRepository) }),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    val warningsOnly by vm.warningsOnly.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { if (state == UiState.Loading) vm.refresh() }

    Scaffold(
        bottomBar = { DataFreshness(state) },
        topBar = {
            TopAppBar(
                title = { Text(stringResource(R.string.events_title)) },
                navigationIcon = { BackButton(onBack) },
                actions = { TooltipIconButton(Icons.Outlined.Refresh, stringResource(R.string.common_refresh), onClick = { vm.refresh() }) },
            )
        },
    ) { padding ->
        Column(Modifier.pageContent(padding).fillMaxSize()) {
            Row(Modifier.padding(horizontal = 16.dp, vertical = 8.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                FilterChip(selected = warningsOnly, onClick = { vm.choose(true) }, label = { Text(stringResource(R.string.kube_events_warnings_only)) })
                FilterChip(selected = !warningsOnly, onClick = { vm.choose(false) }, label = { Text(stringResource(R.string.nodes_filter_all)) })
            }
            HorizontalDivider()
            val rest = Modifier.weight(1f)
            when (val s = state) {
                UiState.Loading -> LoadingBox(rest)
                is UiState.Failed -> ErrorBox(s.message, vm::refresh, rest)
                is UiState.Loaded -> PullToRefreshBox(isRefreshing = s.refreshing, onRefresh = vm::refresh, modifier = rest) {
                    val now = remember(s.data) { System.currentTimeMillis() }
                    when {
                        s.data.forbidden -> EmptyText(stringResource(R.string.kube_events_forbidden))
                        s.data.events.isEmpty() -> EmptyText(stringResource(R.string.kube_events_empty))
                        else -> LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                            // In the server's order (newest first); an index key, as the same event can repeat.
                            itemsIndexed(s.data.events, key = { i, _ -> i }) { _, e -> KubeEventRow(e, now, showObject = true, showNamespace = true) }
                        }
                    }
                }
            }
        }
    }
}
