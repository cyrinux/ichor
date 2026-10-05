package name.levis.ichor.ui.workloads

import android.widget.Toast
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.receiveAsFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.ui.argocd.ArgoRevertDialog
import name.levis.ichor.ui.argocd.ArgoSelfHealer
import name.levis.ichor.ui.argocd.argoSelfHealer
import name.levis.ichor.ui.argocd.freezeForHandChange
import name.levis.ichor.data.cronJobsKey
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.model.KubeCronJob
import name.levis.ichor.model.cronNamespaces
import name.levis.ichor.model.filteredCronJobs
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.components.ConfirmDialog
import name.levis.ichor.ui.components.DataFreshness
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.emptyOrNoMatch
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.uiText

/** Outcome of a manual run, shown once: the new Job's name, or why it failed. */
data class CronRunResult(val cronJob: KubeCronJob, val job: String, val error: UiText?)

class CronJobsViewModel(talos: TalosRepository, metered: () -> Boolean) : PagedListViewModel<KubeCronJob>(talos, metered) {
    override fun key(namespace: String?) = cronJobsKey(namespace)
    override suspend fun page(namespace: String?, token: String) = talos.cronJobsPage(namespace, token)

    private val _triggering = MutableStateFlow<Set<String>>(emptySet())
    /** Keys of the CronJobs whose run request is in flight. */
    val triggering: StateFlow<Set<String>> = _triggering.asStateFlow()

    // A queue, not a state: two runs finishing together each get their message.
    private val _results = Channel<CronRunResult>(Channel.BUFFERED)
    val results: Flow<CronRunResult> = _results.receiveAsFlow()

    fun trigger(cronJob: KubeCronJob) {
        if (cronJob.key in _triggering.value) return
        _triggering.update { it + cronJob.key }
        viewModelScope.launch {
            val outcome = runCatching { talos.triggerCronJob(cronJob) }
            _triggering.update { it - cronJob.key }
            _results.send(CronRunResult(cronJob, outcome.getOrDefault(""), outcome.exceptionOrNull()?.uiText()))
            // The new Job shows as running at once.
            if (outcome.isSuccess) refresh()
        }
    }

    private val _suspending = MutableStateFlow<Set<String>>(emptySet())
    /** Keys of the CronJobs whose suspend or resume is in flight. */
    val suspending: StateFlow<Set<String>> = _suspending.asStateFlow()

    private val _suspendFailures = Channel<UiText>(Channel.BUFFERED)
    /** Why a suspend or resume failed, shown once. */
    val suspendFailures: Flow<UiText> = _suspendFailures.receiveAsFlow()

    /** The Argo CD app that would undo a suspend or resume of [cronJob], from the status already loaded. */
    fun argoHealer(cronJob: KubeCronJob): ArgoSelfHealer? = talos.argoSelfHealer("CronJob", cronJob.namespace, cronJob.name)

    /**
     * Suspends (no new runs) or resumes [cronJob], like `kubectl patch` of spec.suspend; with
     * [freezeFirst], its Argo CD app is frozen for an hour first ([reason] recorded with it).
     */
    fun setSuspended(cronJob: KubeCronJob, suspend: Boolean, freezeFirst: ArgoSelfHealer? = null, reason: String = "") {
        if (cronJob.key in _suspending.value) return
        _suspending.update { it + cronJob.key }
        viewModelScope.launch {
            freezeFirst?.let { healer ->
                cancellableCatching { talos.freezeForHandChange(healer, reason) }.exceptionOrNull()?.let {
                    _suspending.update { it - cronJob.key }
                    _suspendFailures.send(UiText.Res(R.string.argo_freeze_failed_nothing_changed, healer.app.name, it.uiText()))
                    return@launch
                }
            }
            val outcome = cancellableCatching { talos.suspendCronJob(cronJob, suspend) }
            _suspending.update { it - cronJob.key }
            outcome.exceptionOrNull()?.let {
                val action = if (suspend) R.string.cronjobs_suspend_failed else R.string.cronjobs_resume_failed
                _suspendFailures.send(UiText.Res(action, cronJob.displayName, it.uiText()))
            }
            if (outcome.isSuccess) refresh()
        }
    }
}

/**
 * The CronJobs of the scope's namespace, each on a card with its icon (the
 * ichor.levis.name/icon label, else guessed from its image, else a clock), schedule, next run
 * and recent runs, and a button to run it now like `kubectl create job --from`. The scope
 * ([control]) and [query] are shared with the other tabs.
 */
@Composable
fun CronJobsTab(
    control: KubeScopeControl,
    query: String,
    onQuery: (String) -> Unit,
    vm: CronJobsViewModel,
    modifier: Modifier = Modifier,
) {
    val state by vm.state.collectAsStateWithLifecycle()
    val progress by vm.progress.collectAsStateWithLifecycle()
    val triggering by vm.triggering.collectAsStateWithLifecycle()
    val suspending by vm.suspending.collectAsStateWithLifecycle()
    LaunchedEffect(control.scope, control.ready) { if (control.ready) vm.setScope(control.scope) }
    var confirm by remember { mutableStateOf<KubeCronJob?>(null) }
    var confirmSuspend by remember { mutableStateOf<KubeCronJob?>(null) }
    var expanded by remember { mutableStateOf<Set<String>>(emptySet()) }

    CronRunToasts(vm.results)
    val context = LocalContext.current
    LaunchedEffect(vm) { vm.suspendFailures.collect { Toast.makeText(context, it.resolve(context), Toast.LENGTH_LONG).show() } }

    confirmSuspend?.let { c ->
        val suspend = !c.suspended
        val title = stringResource(if (suspend) R.string.cronjobs_suspend_title else R.string.cronjobs_resume_title, c.displayName)
        val text = stringResource(if (suspend) R.string.cronjobs_suspend_text else R.string.cronjobs_resume_text, c.namespace)
        val confirmLabel = stringResource(if (suspend) R.string.cronjobs_suspend else R.string.cronjobs_resume)
        // Argo CD self-heal would put spec.suspend back: offer to freeze its app first.
        val healer = remember(c.key) { vm.argoHealer(c) }
        if (healer != null) {
            val reason = stringResource(if (suspend) R.string.argo_freeze_reason_suspend else R.string.argo_freeze_reason_resume)
            ArgoRevertDialog(
                healer,
                title = title,
                text = text,
                confirm = confirmLabel,
                onFreezeFirst = {
                    confirmSuspend = null
                    vm.setSuspended(c, suspend, freezeFirst = healer, reason = reason)
                },
                onAnyway = {
                    confirmSuspend = null
                    vm.setSuspended(c, suspend)
                },
                onDismiss = { confirmSuspend = null },
            )
        } else {
            ConfirmDialog(
                title = title,
                text = text,
                confirm = confirmLabel,
                onConfirm = {
                    confirmSuspend = null
                    vm.setSuspended(c, suspend)
                },
                onDismiss = { confirmSuspend = null },
            )
        }
    }

    confirm?.let { c ->
        CronRunConfirmDialog(
            cronJob = c,
            onConfirm = {
                confirm = null
                vm.trigger(c)
            },
            onDismiss = { confirm = null },
        )
    }

    KubeListFrame(control, state, { it.cronNamespaces }, query, onQuery, vm::refresh, modifier, placeholder = R.string.cronjobs_search) { s ->
        val load = s.data
        val selected = control.scope.namespace
        val rows = remember(load, selected, query) { load.items.filteredCronJobs(selected, query, sorted = load.done) }
        PagedProgress(progress)
        IncompleteNotice(load, searching = query.isNotBlank(), onLoadMore = vm::loadMore, onLoadAll = vm::loadAll)
        PullToRefreshBox(isRefreshing = s.refreshing, onRefresh = vm::refresh, modifier = Modifier.weight(1f)) {
            if (rows.isEmpty()) {
                EmptyText(emptyOrNoMatch(query, R.string.cronjobs_empty, R.string.cronjobs_no_match))
            } else {
                val listState = rememberLazyListState()
                LoadMoreOnScroll(listState, enabled = load.hasMore && query.isBlank(), loaded = load.items.size, onLoadMore = vm::loadMore)
                LazyColumn(
                    Modifier.fillMaxSize(),
                    state = listState,
                    contentPadding = PaddingValues(horizontal = 12.dp, vertical = 10.dp),
                    verticalArrangement = Arrangement.spacedBy(10.dp),
                ) {
                    items(rows, key = { it.key }) { c ->
                        CronJobCard(
                            cronJob = c,
                            showNamespace = selected == null,
                            expanded = c.key in expanded,
                            triggering = c.key in triggering,
                            suspending = c.key in suspending,
                            onToggle = { expanded = if (c.key in expanded) expanded - c.key else expanded + c.key },
                            onRun = { confirm = c },
                            onSuspend = { confirmSuspend = c },
                        )
                    }
                }
            }
        }
        DataFreshness(s, edgeToEdge = false)
    }
}

/** A toast for each manual run outcome of [results]. */
@Composable
private fun CronRunToasts(results: Flow<CronRunResult>) {
    val context = LocalContext.current
    LaunchedEffect(results) {
        results.collect { r ->
            val text = r.error?.resolve(context)?.let { context.getString(R.string.cronjobs_run_failed, r.cronJob.displayName, it) }
                ?: context.getString(R.string.cronjobs_run_done, r.job.ifEmpty { r.cronJob.name })
            Toast.makeText(context, text, if (r.error == null) Toast.LENGTH_SHORT else Toast.LENGTH_LONG).show()
        }
    }
}

@Composable
private fun CronRunConfirmDialog(cronJob: KubeCronJob, onConfirm: () -> Unit, onDismiss: () -> Unit) {
    val warn = LocalStatusColors.current.warn
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.cronjobs_run_title, cronJob.displayName)) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(stringResource(R.string.cronjobs_run_text, cronJob.namespace))
                if (cronJob.active > 0) Text(stringResource(R.string.cronjobs_run_active), color = warn)
                if (cronJob.suspended) Text(stringResource(R.string.cronjobs_run_suspended), color = warn)
            }
        },
        confirmButton = { TextButton(onClick = onConfirm) { Text(stringResource(R.string.cronjobs_run_confirm)) } },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}
