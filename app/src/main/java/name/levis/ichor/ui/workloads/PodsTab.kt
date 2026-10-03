package name.levis.ichor.ui.workloads

import android.widget.Toast
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
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
import androidx.compose.ui.res.pluralStringResource
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
import name.levis.ichor.data.PODS
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.model.KubePod
import name.levis.ichor.model.filteredPods
import name.levis.ichor.model.podNamespaces
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

/** Outcome of a pod deletion, shown once. */
data class DeleteResult(val pod: KubePod, val error: UiText?)

class PodsViewModel(private val talos: TalosRepository) : LoadingViewModel<List<KubePod>>() {
    override val keepsDataOnFailure = true
    override fun cached(): TalosRepository.Timed<List<KubePod>>? = talos.cached(PODS)
    override suspend fun fetch() = talos.pods()

    private val _deleting = MutableStateFlow<Set<String>>(emptySet())
    /** Keys of the pods whose deletion is in flight. */
    val deleting: StateFlow<Set<String>> = _deleting.asStateFlow()

    private val _results = Channel<DeleteResult>(Channel.BUFFERED)
    val results: Flow<DeleteResult> = _results.receiveAsFlow()

    fun delete(pod: KubePod) {
        if (pod.key in _deleting.value) return
        _deleting.update { it + pod.key }
        viewModelScope.launch {
            val outcome = runCatching { talos.deletePod(pod) }
            _deleting.update { it - pod.key }
            _results.send(DeleteResult(pod, outcome.exceptionOrNull()?.uiText()))
            // Shows it terminating, and soon its replacement.
            if (outcome.isSuccess) refresh()
        }
    }
}

/**
 * Every pod of the cluster with the status `kubectl get pods` shows, unhealthy ones first,
 * and a delete action so a controller starts a fresh one.
 */
@Composable
fun PodsTab(
    namespace: String?,
    query: String,
    onNamespace: (String?) -> Unit,
    onQuery: (String) -> Unit,
    modifier: Modifier = Modifier,
    vm: PodsViewModel = viewModel(factory = factory { PodsViewModel(app.talosRepository) }),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    val deleting by vm.deleting.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { if (state == UiState.Loading) vm.refresh() }
    var confirm by remember { mutableStateOf<KubePod?>(null) }

    val context = LocalContext.current
    LaunchedEffect(vm) {
        vm.results.collect { r ->
            val text = r.error?.resolve(context)?.let { context.getString(R.string.pods_delete_failed, r.pod.name, it) }
                ?: context.getString(R.string.pods_delete_done, r.pod.name)
            Toast.makeText(context, text, if (r.error == null) Toast.LENGTH_SHORT else Toast.LENGTH_LONG).show()
        }
    }

    confirm?.let { pod ->
        DeleteConfirmDialog(
            pod = pod,
            onConfirm = {
                confirm = null
                vm.delete(pod)
            },
            onDismiss = { confirm = null },
        )
    }

    when (val s = state) {
        UiState.Loading -> LoadingBox(modifier)
        is UiState.Failed -> ErrorBox(s.message, vm::refresh, modifier)
        is UiState.Loaded -> Column(modifier.fillMaxSize()) {
            val namespaces = remember(s.data) { s.data.podNamespaces }
            val selected = namespace?.takeIf { it in namespaces }
            val rows = remember(s.data, selected, query) { s.data.filteredPods(selected, query) }
            KubeFilters(namespaces, selected, query, onNamespace, onQuery)
            HorizontalDivider()
            PullToRefreshBox(isRefreshing = s.refreshing, onRefresh = vm::refresh, modifier = Modifier.weight(1f)) {
                if (rows.isEmpty()) {
                    Text(
                        if (query.isBlank()) stringResource(R.string.pods_empty) else stringResource(R.string.pods_no_match, query.trim()),
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.padding(16.dp),
                    )
                } else {
                    LazyColumn(Modifier.fillMaxSize()) {
                        items(rows, key = { it.key }) { pod ->
                            PodRow(pod, showNamespace = selected == null, deleting = pod.key in deleting, onDelete = { confirm = pod })
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
private fun PodRow(pod: KubePod, showNamespace: Boolean, deleting: Boolean, onDelete: () -> Unit) {
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    Row(Modifier.fillMaxWidth().padding(start = 16.dp, end = 4.dp, top = 6.dp, bottom = 6.dp), verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f)) {
            Text(pod.name, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Text(
                listOfNotNull(pod.namespace.takeIf { showNamespace }, pod.node.takeIf { it.isNotEmpty() }).joinToString("  ·  "),
                style = MaterialTheme.typography.labelSmall,
                color = muted,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(pod.status, style = MaterialTheme.typography.labelSmall, fontFamily = FontFamily.Monospace, color = pod.statusColor())
                Text(stringResource(R.string.pods_ready_count, pod.ready, pod.containers), style = MaterialTheme.typography.labelSmall, color = muted)
                if (pod.restarts > 0) {
                    Text(
                        pluralStringResource(R.plurals.pods_restarts, pod.restarts, pod.restarts),
                        style = MaterialTheme.typography.labelSmall,
                        color = if (pod.healthy) muted else LocalStatusColors.current.warn,
                    )
                }
            }
        }
        if (deleting) {
            CircularProgressIndicator(Modifier.padding(12.dp).size(24.dp), strokeWidth = 2.dp)
        } else {
            IconButton(onClick = onDelete, enabled = pod.status != "Terminating") {
                Icon(Icons.Outlined.Delete, stringResource(R.string.pods_delete, pod.name))
            }
        }
    }
}

@Composable
private fun DeleteConfirmDialog(pod: KubePod, onConfirm: () -> Unit, onDismiss: () -> Unit) {
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.pods_delete_title, pod.name)) },
        text = {
            Text(
                if (pod.owner.isNotEmpty()) {
                    stringResource(R.string.pods_delete_text_owned, pod.namespace, pod.owner)
                } else {
                    stringResource(R.string.pods_delete_text_bare, pod.namespace)
                },
            )
        },
        confirmButton = {
            TextButton(onClick = onConfirm) { Text(stringResource(R.string.pods_delete_confirm), color = LocalStatusColors.current.bad) }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}

@Composable
private fun KubePod.statusColor(): Color {
    val colors = LocalStatusColors.current
    return when {
        healthy -> colors.ok
        transitional -> colors.warn
        else -> colors.bad
    }
}
