package name.levis.ichor.ui.workloads

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.RestartAlt
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import name.levis.ichor.ui.design.IchorIconButton
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
import name.levis.ichor.data.GitOpsRepository
import name.levis.ichor.data.KubeRepository
import name.levis.ichor.data.isMeteredNetwork
import name.levis.ichor.data.workloadsKey
import name.levis.ichor.model.KubeAction
import name.levis.ichor.model.KubeRevision
import name.levis.ichor.model.KubeRevisionList
import name.levis.ichor.model.KubeWorkload
import name.levis.ichor.model.PodSelection
import name.levis.ichor.model.podSelection
import name.levis.ichor.model.WorkloadState
import name.levis.ichor.model.fetchWorkloadPage
import name.levis.ichor.model.filtered
import name.levis.ichor.model.namespaces
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.CardListPadding
import name.levis.ichor.ui.components.rememberKubeActionAccess
import name.levis.ichor.ui.components.DataFreshness
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.KubeDenialNote
import name.levis.ichor.ui.components.rememberFirstKubeDenial
import name.levis.ichor.ui.components.rememberKubeDenial
import name.levis.ichor.ui.components.emptyOrNoMatch
import name.levis.ichor.ui.components.groupedCardRows
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.timeAgo
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.flow.filter
import name.levis.ichor.data.KUBE_WATCH_RETRY_MILLIS
import name.levis.ichor.data.StreamItem
import name.levis.ichor.data.watchForever
import name.levis.ichor.model.WORKLOAD_WATCH_KINDS
import name.levis.ichor.ui.PollWhileStarted

class WorkloadsViewModel(kube: KubeRepository, gitOps: GitOpsRepository, metered: () -> Boolean) :
    PagedListViewModel<KubeWorkload>(kube, metered) {
    override fun key(namespace: String?) = workloadsKey(namespace)

    // The three kinds side by side, one page each, merged.
    override suspend fun page(namespace: String?, token: String) =
        fetchWorkloadPage(token) { kind, kindToken -> kube.workloadsPage(kind, namespace, kindToken) }

    // Show the rollout starting: the controller already bumped the generation.
    val restarts = WorkloadRestarts(viewModelScope, kube) { refresh() }

    val actions = WorkloadActions(viewModelScope, kube, gitOps, restarts) { refresh() }

    /**
     * Reads the list again while called (the screen is visible) each time a workload of the
     * scope changes, instead of waiting for pull-to-refresh: the Go core signals at most every
     * 2 s, never for the lists it reads at the start. Restarted after each load (a refresh, a
     * scope change), skipped while one runs; a watch that ends is followed again after
     * [KUBE_WATCH_RETRY_MILLIS].
     */
    suspend fun follow() {
        settled.filter { it > 0 }.collectLatest {
            val namespace = scope.namespace
            watchForever(start = { kube.changeWatch(namespace, WORKLOAD_WATCH_KINDS) }) { item ->
                val refreshing = (state.value as? UiState.Loaded)?.refreshing == true
                if (item is StreamItem.Item && !refreshing) refresh()
            }
        }
    }
}

/**
 * Deployments, StatefulSets and DaemonSets of the scope's namespace with a rolling restart like
 * `kubectl rollout restart`; a tap opens the workload's sheet (scale, history and rollback).
 * The scope ([control]) and [query] are shared with the Pods tab. [focusKey]: the workload
 * whose sheet to open once listed (a share link), then [onFocused].
 */
@Composable
fun WorkloadsTab(
    control: KubeScopeControl,
    query: String,
    onQuery: (String) -> Unit,
    modifier: Modifier = Modifier,
    onAutoscaling: () -> Unit = {},
    vm: WorkloadsViewModel = viewModel(factory = factory { WorkloadsViewModel(app.kubeRepository, app.gitOpsRepository) { isMeteredNetwork(app) } }),
    focusKey: String? = null,
    onFocused: () -> Unit = {},
) {
    val state by vm.state.collectAsStateWithLifecycle()
    val progress by vm.progress.collectAsStateWithLifecycle()
    val restarting by vm.restarts.restarting.collectAsStateWithLifecycle()
    LaunchedEffect(control.scope, control.ready) { if (control.ready) vm.setScope(control.scope) }
    PollWhileStarted { vm.follow() }
    var confirm by remember { mutableStateOf<KubeWorkload?>(null) }
    var opened by remember { mutableStateOf<String?>(null) }
    var revisionOf by remember { mutableStateOf<Pair<KubeRevision, KubeRevisionList>?>(null) }
    var podsOf by remember { mutableStateOf<PodSelection.OfWorkload?>(null) }
    val listed = (state as? UiState.Loaded)?.data?.items
    LaunchedEffect(focusKey, listed) {
        if (focusKey != null && listed.orEmpty().any { it.key == focusKey }) {
            opened = focusKey
            onFocused()
        }
    }
    podsOf?.let { WorkloadPodsSheet(it, onDismiss = { podsOf = null }) }

    RestartResultToasts(vm.restarts.results)
    val followRollout = rememberRolloutFollow()
    ActionMessageToasts(vm.actions.messages)
    RolloutStatusSheet(vm.restarts)

    // The live row, so the sheet follows a refresh (new replica count, new revision).
    val openedWorkload = (state as? UiState.Loaded)?.data?.items?.firstOrNull { it.key == opened }
    // A revision's diff replaces the workload's sheet; closing it goes back to the sheet.
    val shownRevision = revisionOf
    if (openedWorkload != null && shownRevision != null) {
        val (revision, history) = shownRevision
        val busy by vm.actions.busy.collectAsStateWithLifecycle()
        RevisionDiffSheet(
            workload = openedWorkload,
            revision = revision,
            manual = history.manual,
            paused = history.paused,
            actions = vm.actions,
            denial = rememberKubeActionAccess(openedWorkload.namespace)?.denial(KubeAction.rollback(openedWorkload.kind)),
            busy = openedWorkload.key in busy,
            onRollback = {
                revisionOf = null
                // The rollout sheet takes over once the rollback is accepted.
                opened = null
                vm.actions.rollback(openedWorkload, revision, history.manual)
            },
            onDismiss = { revisionOf = null },
        )
    }
    openedWorkload?.takeIf { shownRevision == null }?.let { w ->
        WorkloadSheet(
            workload = w,
            actions = vm.actions,
            onRestart = { confirm = w },
            onRevision = { revision, history -> revisionOf = revision to history },
            onDismiss = { opened = null },
            onAutoscaling = { opened = null; onAutoscaling() },
            onPods = w.podSelection?.let { selection ->
                {
                    // One sheet at a time: the pods replace the workload's.
                    opened = null
                    podsOf = selection
                }
            },
        )
    }
    confirm?.let { w ->
        RestartConfirmDialog(
            workload = w,
            onConfirm = {
                confirm = null
                vm.restarts.restart(w, followRollout)
            },
            onDismiss = { confirm = null },
        )
    }

    KubeListFrame(control, state, { it.namespaces }, query, onQuery, vm::refresh, modifier) { s ->
        val load = s.data
        val selected = control.scope.namespace
        val rows = remember(load, selected, query) { load.items.filtered(selected, query, sorted = load.done) }
        PagedProgress(progress)
        // Each kind listed has its own restart permission: the first one refused stands for the list.
        val restarts = remember(rows, selected) { rows.map { KubeAction.restart(it.kind) to selected.orEmpty() }.distinct() }
        KubeDenialNote(rememberFirstKubeDenial(restarts), Modifier.padding(horizontal = 16.dp, vertical = 4.dp))
        IncompleteNotice(load, searching = query.isNotBlank(), onLoadMore = vm::loadMore, onLoadAll = vm::loadAll)
        PullToRefreshBox(isRefreshing = s.refreshing, onRefresh = vm::refresh, modifier = Modifier.weight(1f)) {
            if (rows.isEmpty()) {
                EmptyText(emptyOrNoMatch(query, R.string.workloads_empty, R.string.workloads_no_match))
            } else {
                val listState = rememberLazyListState()
                LoadMoreOnScroll(listState, enabled = load.hasMore && query.isBlank(), loaded = load.items.size, onLoadMore = vm::loadMore)
                // All namespaces: one card per namespace, its name as the card's title.
                LazyColumn(Modifier.fillMaxSize(), state = listState, contentPadding = CardListPadding) {
                    groupedCardRows(rows, groupOf = if (selected == null) { w: KubeWorkload -> w.namespace } else null, key = { it.key }) { w ->
                        WorkloadRow(
                            w,
                            restarting = w.key in restarting,
                            onRestart = { confirm = w },
                            onOpen = { opened = w.key },
                            autoscaled = w.autoscaler != null,
                            autoscalerStateOk = w.autoscaler?.stateOk ?: true,
                        )
                    }
                }
            }
        }
        DataFreshness(s, edgeToEdge = false)
    }
}

/** A workload: tap for its sheet, swipe right to restart, long press for Restart, Scale…, Pin and Share. */
@Composable
private fun WorkloadRow(workload: KubeWorkload, restarting: Boolean, onRestart: () -> Unit, onOpen: () -> Unit, autoscaled: Boolean = false, autoscalerStateOk: Boolean = true) {
    WorkloadQuickActions(workload, restarting, onRestart = onRestart, onOpen = onOpen) {
        WorkloadRowContent(workload, restarting, onRestart, autoscaled, autoscalerStateOk)
    }
}

@Composable
private fun WorkloadRowContent(workload: KubeWorkload, restarting: Boolean, onRestart: () -> Unit, autoscaled: Boolean, autoscalerStateOk: Boolean) {
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    Row(Modifier.fillMaxWidth().padding(start = 16.dp, end = 4.dp, top = 6.dp, bottom = 6.dp), verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f)) {
            Text(
                workload.name,
                style = MaterialTheme.typography.bodyMedium,
                fontFamily = FontFamily.Monospace,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            Text(
                workload.kind,
                style = MaterialTheme.typography.labelSmall,
                color = muted,
            )
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(
                    stringResource(R.string.workloads_ready_count, workload.ready, workload.desired) + " · " + stringResource(workload.workloadState.label),
                    style = MaterialTheme.typography.labelSmall,
                    color = workload.workloadState.color(),
                )
                if (autoscaled) {
                    Text(
                        stringResource(R.string.workloads_autoscaled_badge),
                        style = MaterialTheme.typography.labelSmall,
                        color = if (autoscalerStateOk) muted else MaterialTheme.colorScheme.error,
                    )
                }
                if (workload.restartedAt > 0) {
                    val ago = timeAgo(workload.restartedAt)
                    Text(stringResource(R.string.workloads_restarted_ago, ago), style = MaterialTheme.typography.labelSmall, color = muted)
                }
            }
        }
        if (restarting) {
            CircularProgressIndicator(Modifier.padding(12.dp).size(24.dp), strokeWidth = 2.dp)
        } else {
            val denied = rememberKubeDenial(KubeAction.restart(workload.kind), workload.namespace) != null
            IchorIconButton(onClick = onRestart, enabled = workload.canRestart && !denied) {
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
