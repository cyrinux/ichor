package name.levis.ichor.ui.workloads

import android.text.format.DateUtils
import android.widget.Toast
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.RestartAlt
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.FilterChip
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
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
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.compose.viewModel
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.receiveAsFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.data.WORKLOADS
import name.levis.ichor.model.KubeWorkload
import name.levis.ichor.model.WorkloadState
import name.levis.ichor.model.filtered
import name.levis.ichor.model.namespaces
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.DataFreshness
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.uiText

/** Outcome of a rollout restart, shown once. */
data class RestartResult(val workload: KubeWorkload, val error: UiText?)

class WorkloadsViewModel(private val talos: TalosRepository) : LoadingViewModel<List<KubeWorkload>>() {
    override val keepsDataOnFailure = true
    override fun cached(): TalosRepository.Timed<List<KubeWorkload>>? = talos.cached(WORKLOADS)
    override val restores get() = talos.restores
    override suspend fun fetch() = talos.workloads()

    private val _restarting = MutableStateFlow<Set<String>>(emptySet())
    /** Keys of the workloads whose restart request is in flight. */
    val restarting: StateFlow<Set<String>> = _restarting.asStateFlow()

    // A queue, not a state: two restarts finishing together each get their message.
    private val _results = Channel<RestartResult>(Channel.BUFFERED)
    val results: Flow<RestartResult> = _results.receiveAsFlow()

    fun restart(workload: KubeWorkload) {
        if (workload.key in _restarting.value) return
        _restarting.update { it + workload.key }
        viewModelScope.launch {
            val outcome = runCatching { talos.rolloutRestart(workload) }
            _restarting.update { it - workload.key }
            _results.send(RestartResult(workload, outcome.exceptionOrNull()?.uiText()))
            // Show the rollout starting: the controller already bumped the generation.
            if (outcome.isSuccess) refresh()
        }
    }
}

/**
 * Deployments, StatefulSets and DaemonSets of the cluster with a rolling restart like
 * `kubectl rollout restart`. [namespace] and [query] are shared with the Pods tab.
 */
@Composable
fun WorkloadsTab(
    namespace: String?,
    query: String,
    onNamespace: (String?) -> Unit,
    onQuery: (String) -> Unit,
    modifier: Modifier = Modifier,
    vm: WorkloadsViewModel = viewModel(factory = factory { WorkloadsViewModel(app.talosRepository) }),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    val restarting by vm.restarting.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { if (state == UiState.Loading) vm.refresh() }
    var confirm by remember { mutableStateOf<KubeWorkload?>(null) }

    val context = LocalContext.current
    LaunchedEffect(vm) {
        vm.results.collect { r ->
            val text = r.error?.resolve(context)?.let { context.getString(R.string.workloads_restart_failed, r.workload.name, it) }
                ?: context.getString(R.string.workloads_restart_done, r.workload.name)
            Toast.makeText(context, text, if (r.error == null) Toast.LENGTH_SHORT else Toast.LENGTH_LONG).show()
        }
    }

    confirm?.let { w ->
        RestartConfirmDialog(
            workload = w,
            onConfirm = {
                confirm = null
                vm.restart(w)
            },
            onDismiss = { confirm = null },
        )
    }

    when (val s = state) {
        UiState.Loading -> LoadingBox(modifier)
        is UiState.Failed -> ErrorBox(s.message, vm::refresh, modifier)
        is UiState.Loaded -> Column(modifier.fillMaxSize()) {
            val namespaces = remember(s.data) { s.data.namespaces }
            val selected = namespace?.takeIf { it in namespaces }
            val rows = remember(s.data, selected, query) { s.data.filtered(selected, query) }
            KubeFilters(namespaces, selected, query, onNamespace, onQuery)
            HorizontalDivider()
            PullToRefreshBox(isRefreshing = s.refreshing, onRefresh = vm::refresh, modifier = Modifier.weight(1f)) {
                if (rows.isEmpty()) {
                    Text(
                        if (query.isBlank()) stringResource(R.string.workloads_empty) else stringResource(R.string.workloads_no_match, query.trim()),
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.padding(16.dp),
                    )
                } else {
                    LazyColumn(Modifier.fillMaxSize()) {
                        items(rows, key = { it.key }) { w ->
                            WorkloadRow(w, showNamespace = selected == null, restarting = w.key in restarting, onRestart = { confirm = w })
                            HorizontalDivider()
                        }
                    }
                }
            }
            DataFreshness(s, edgeToEdge = false)
        }
    }
}

/** Search field and namespace chips, shared by the Workloads and Pods tabs. */
@Composable
internal fun KubeFilters(
    namespaces: List<String>,
    selected: String?,
    query: String,
    onNamespace: (String?) -> Unit,
    onQuery: (String) -> Unit,
) {
    Column(Modifier.padding(vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
        OutlinedTextField(
            value = query,
            onValueChange = onQuery,
            placeholder = { Text(stringResource(R.string.workloads_search)) },
            leadingIcon = { Icon(Icons.Outlined.Search, contentDescription = null) },
            singleLine = true,
            modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp),
        )
        LazyRow(contentPadding = PaddingValues(horizontal = 16.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            item {
                FilterChip(selected = selected == null, onClick = { onNamespace(null) }, label = { Text(stringResource(R.string.workloads_all_namespaces)) })
            }
            items(namespaces, key = { it }) { ns ->
                FilterChip(selected = selected == ns, onClick = { onNamespace(ns) }, label = { Text(ns, fontFamily = FontFamily.Monospace) })
            }
        }
    }
}

@Composable
private fun WorkloadRow(workload: KubeWorkload, showNamespace: Boolean, restarting: Boolean, onRestart: () -> Unit) {
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

@Composable
private fun RestartConfirmDialog(workload: KubeWorkload, onConfirm: () -> Unit, onDismiss: () -> Unit) {
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.workloads_restart_title, workload.kind, workload.name)) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(stringResource(R.string.workloads_restart_text, workload.namespace))
                if (workload.desired <= 1) {
                    Text(stringResource(R.string.workloads_restart_single), color = LocalStatusColors.current.warn)
                }
            }
        },
        confirmButton = {
            TextButton(onClick = onConfirm) { Text(stringResource(R.string.workloads_restart_confirm), color = LocalStatusColors.current.bad) }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
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
