package name.levis.ichor.ui.workloads

import android.widget.Toast
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
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
import name.levis.ichor.data.CRON_JOBS
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.model.KubeCronJob
import name.levis.ichor.model.cronNamespaces
import name.levis.ichor.model.filteredCronJobs
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.components.DataFreshness
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.emptyOrNoMatch
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.uiText

/** Outcome of a manual run, shown once: the new Job's name, or why it failed. */
data class CronRunResult(val cronJob: KubeCronJob, val job: String, val error: UiText?)

class CronJobsViewModel(private val talos: TalosRepository) : LoadingViewModel<List<KubeCronJob>>() {
    override val keepsDataOnFailure = true
    override fun cached(): TalosRepository.Timed<List<KubeCronJob>>? = talos.cached(CRON_JOBS)
    override val restores get() = talos.restores
    override suspend fun fetch() = talos.cronJobs()

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
}

/**
 * The cluster's CronJobs, each on a card with its icon (the ichor.levis.name/icon label,
 * else guessed from its image, else a clock), schedule, next run and recent runs, and a
 * button to run it now like `kubectl create job --from`. [namespace] and [query] are shared
 * with the other tabs.
 */
@Composable
fun CronJobsTab(
    namespace: String?,
    query: String,
    onNamespace: (String?) -> Unit,
    onQuery: (String) -> Unit,
    vm: CronJobsViewModel,
    modifier: Modifier = Modifier,
) {
    val state by vm.state.collectAsStateWithLifecycle()
    val triggering by vm.triggering.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { if (state == UiState.Loading) vm.refresh() }
    var confirm by remember { mutableStateOf<KubeCronJob?>(null) }
    var expanded by remember { mutableStateOf<Set<String>>(emptySet()) }

    CronRunToasts(vm.results)

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

    when (val s = state) {
        UiState.Loading -> LoadingBox(modifier)
        is UiState.Failed -> ErrorBox(s.message, vm::refresh, modifier)
        is UiState.Loaded -> Column(modifier.fillMaxSize()) {
            val namespaces = remember(s.data) { s.data.cronNamespaces }
            val selected = namespace?.takeIf { it in namespaces }
            val rows = remember(s.data, selected, query) { s.data.filteredCronJobs(selected, query) }
            KubeFilters(namespaces, selected, query, onNamespace, onQuery, placeholder = R.string.cronjobs_search)
            HorizontalDivider()
            PullToRefreshBox(isRefreshing = s.refreshing, onRefresh = vm::refresh, modifier = Modifier.weight(1f)) {
                if (rows.isEmpty()) {
                    EmptyText(emptyOrNoMatch(query, R.string.cronjobs_empty, R.string.cronjobs_no_match))
                } else {
                    LazyColumn(
                        Modifier.fillMaxSize(),
                        contentPadding = PaddingValues(horizontal = 12.dp, vertical = 10.dp),
                        verticalArrangement = Arrangement.spacedBy(10.dp),
                    ) {
                        items(rows, key = { it.key }) { c ->
                            CronJobCard(
                                cronJob = c,
                                showNamespace = selected == null,
                                expanded = c.key in expanded,
                                triggering = c.key in triggering,
                                onToggle = { expanded = if (c.key in expanded) expanded - c.key else expanded + c.key },
                                onRun = { confirm = c },
                            )
                        }
                    }
                }
            }
            DataFreshness(s, edgeToEdge = false)
        }
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
