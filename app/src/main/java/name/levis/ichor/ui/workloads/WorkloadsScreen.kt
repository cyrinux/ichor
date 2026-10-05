package name.levis.ichor.ui.workloads

import android.text.format.DateUtils
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.RestartAlt
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.R
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.data.isMeteredNetwork
import name.levis.ichor.data.workloadsKey
import name.levis.ichor.model.KubeRevision
import name.levis.ichor.model.KubeWorkload
import name.levis.ichor.model.WorkloadState
import name.levis.ichor.model.fetchWorkloadPage
import name.levis.ichor.model.filtered
import name.levis.ichor.model.namespaces
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.ConfirmDialog
import name.levis.ichor.ui.components.DataFreshness
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.emptyOrNoMatch
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.theme.LocalStatusColors

class WorkloadsViewModel(talos: TalosRepository, metered: () -> Boolean) : PagedListViewModel<KubeWorkload>(talos, metered) {
    override fun key(namespace: String?) = workloadsKey(namespace)

    // The three kinds side by side, one page each, merged.
    override suspend fun page(namespace: String?, token: String) =
        fetchWorkloadPage(token) { kind, kindToken -> talos.workloadsPage(kind, namespace, kindToken) }

    // Show the rollout starting: the controller already bumped the generation.
    val restarts = WorkloadRestarts(viewModelScope, talos) { refresh() }

    val actions = WorkloadActions(viewModelScope, talos, restarts) { refresh() }
}

/**
 * Deployments, StatefulSets and DaemonSets of the scope's namespace with a rolling restart like
 * `kubectl rollout restart`; a tap opens the workload's sheet (scale, history and rollback).
 * The scope ([control]) and [query] are shared with the Pods tab.
 */
@Composable
fun WorkloadsTab(
    control: KubeScopeControl,
    query: String,
    onQuery: (String) -> Unit,
    modifier: Modifier = Modifier,
    vm: WorkloadsViewModel = viewModel(factory = factory { WorkloadsViewModel(app.talosRepository) { isMeteredNetwork(app) } }),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    val progress by vm.progress.collectAsStateWithLifecycle()
    val restarting by vm.restarts.restarting.collectAsStateWithLifecycle()
    LaunchedEffect(control.scope) { vm.setScope(control.scope) }
    var confirm by remember { mutableStateOf<KubeWorkload?>(null) }
    var opened by remember { mutableStateOf<String?>(null) }
    var rollback by remember { mutableStateOf<Pair<KubeWorkload, KubeRevision>?>(null) }

    RestartResultToasts(vm.restarts.results)
    ActionMessageToasts(vm.actions.messages)
    RolloutStatusSheet(vm.restarts)

    // The live row, so the sheet follows a refresh (new replica count, new revision).
    (state as? UiState.Loaded)?.data?.items?.firstOrNull { it.key == opened }?.let { w ->
        WorkloadSheet(
            workload = w,
            actions = vm.actions,
            onRestart = { confirm = w },
            onRollback = { rollback = w to it },
            onDismiss = { opened = null },
        )
    }
    rollback?.let { (w, revision) ->
        ConfirmDialog(
            title = stringResource(R.string.workloads_rollback_title, w.name, revision.revision),
            text = stringResource(R.string.workloads_rollback_text, w.namespace, revision.images.joinToString(", ").ifEmpty { revision.replicaSet }),
            confirm = stringResource(R.string.workloads_rollback),
            onConfirm = {
                rollback = null
                // The rollout sheet takes over once the rollback is accepted.
                opened = null
                vm.actions.rollback(w, revision)
            },
            onDismiss = { rollback = null },
            destructive = true,
        )
    }

    confirm?.let { w ->
        RestartConfirmDialog(
            workload = w,
            onConfirm = {
                confirm = null
                vm.restarts.restart(w)
            },
            onDismiss = { confirm = null },
        )
    }

    when (val s = state) {
        UiState.Loading -> LoadingBox(modifier)
        is UiState.Failed -> ErrorBox(s.message, vm::refresh, modifier)
        is UiState.Loaded -> Column(modifier.fillMaxSize()) {
            val load = s.data
            val loadedNamespaces = remember(load) { load.items.namespaces }
            val selected = control.scope.namespace
            val rows = remember(load, selected, query) { load.items.filtered(selected, query, sorted = load.done) }
            KubeFilters(control, loadedNamespaces, query, onQuery)
            HorizontalDivider()
            PagedProgress(progress)
            IncompleteNotice(load, searching = query.isNotBlank())
            PullToRefreshBox(isRefreshing = s.refreshing, onRefresh = vm::refresh, modifier = Modifier.weight(1f)) {
                if (rows.isEmpty()) {
                    EmptyText(emptyOrNoMatch(query, R.string.workloads_empty, R.string.workloads_no_match))
                } else {
                    val listState = rememberLazyListState()
                    LoadMoreOnScroll(listState, enabled = load.hasMore, loaded = load.items.size, onLoadMore = vm::loadMore)
                    LazyColumn(Modifier.fillMaxSize(), state = listState) {
                        items(rows, key = { it.key }) { w ->
                            WorkloadRow(
                                w,
                                showNamespace = selected == null,
                                restarting = w.key in restarting,
                                onRestart = { confirm = w },
                                onOpen = { opened = w.key },
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

@Composable
private fun WorkloadRow(workload: KubeWorkload, showNamespace: Boolean, restarting: Boolean, onRestart: () -> Unit, onOpen: () -> Unit) {
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    Row(Modifier.fillMaxWidth().clickable(onClick = onOpen).padding(start = 16.dp, end = 4.dp, top = 6.dp, bottom = 6.dp), verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f)) {
            Text(
                workload.name,
                style = MaterialTheme.typography.bodyMedium,
                fontFamily = FontFamily.Monospace,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            Text(
                listOfNotNull(workload.kind, workload.namespace.takeIf { showNamespace }).joinToString("  ·  "),
                style = MaterialTheme.typography.labelSmall,
                color = muted,
            )
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(
                    stringResource(R.string.workloads_ready_count, workload.ready, workload.desired) + " · " + stringResource(workload.workloadState.label),
                    style = MaterialTheme.typography.labelSmall,
                    color = workload.workloadState.color(),
                )
                if (workload.restartedAt > 0) {
                    val ago = DateUtils.getRelativeTimeSpanString(workload.restartedAt, System.currentTimeMillis(), DateUtils.MINUTE_IN_MILLIS)
                    Text(stringResource(R.string.workloads_restarted_ago, ago), style = MaterialTheme.typography.labelSmall, color = muted)
                }
            }
        }
        if (restarting) {
            CircularProgressIndicator(Modifier.padding(12.dp).size(24.dp), strokeWidth = 2.dp)
        } else {
            IconButton(onClick = onRestart, enabled = workload.canRestart) {
                Icon(Icons.Outlined.RestartAlt, stringResource(R.string.workloads_restart, workload.name))
            }
        }
    }
}

private val WorkloadState.label: Int
    get() = when (this) {
        WorkloadState.READY -> R.string.workloads_state_ready
        WorkloadState.PROGRESSING -> R.string.workloads_state_progressing
        WorkloadState.DEGRADED -> R.string.workloads_state_degraded
        WorkloadState.PAUSED -> R.string.workloads_state_paused
        WorkloadState.SCALED_DOWN -> R.string.workloads_state_scaled_down
        WorkloadState.UNKNOWN -> R.string.workloads_state_unknown
    }

@Composable
private fun WorkloadState.color(): Color {
    val colors = LocalStatusColors.current
    return when (this) {
        WorkloadState.READY -> colors.ok
        WorkloadState.PROGRESSING -> colors.warn
        WorkloadState.DEGRADED -> colors.bad
        else -> colors.muted
    }
}
