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
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.Article
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.Stream
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.Stable
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
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.receiveAsFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.data.isMeteredNetwork
import name.levis.ichor.data.podsKey
import name.levis.ichor.data.KubeRepository
import name.levis.ichor.model.KubeAction
import name.levis.ichor.model.KubePod
import name.levis.ichor.model.filteredPods
import name.levis.ichor.model.podNamespaces
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.app
import name.levis.ichor.ui.cancellableCatching
import name.levis.ichor.ui.components.KubeDenialNote
import name.levis.ichor.ui.components.rememberKubeDenial
import name.levis.ichor.ui.components.ConfirmDialog
import name.levis.ichor.ui.components.DataFreshness
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.emptyOrNoMatch
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.uiText

/** Outcome of a pod deletion, shown once. */
data class DeleteResult(val pod: KubePod, val error: UiText?)

class PodsViewModel(kube: KubeRepository, metered: () -> Boolean) : PagedListViewModel<KubePod>(kube, metered) {
    override fun key(namespace: String?) = podsKey(namespace)

    // The first page as full objects: a small cluster, loaded in one page, keeps its images
    // and containers; the next pages as Table rows, 10-20 times smaller (L9, L10).
    override suspend fun page(namespace: String?, token: String) = kube.podsPage(namespace, token, table = token.isNotEmpty())

    // Kept rows of Table pages have no images: image search would miss them.
    override fun detailed(items: List<KubePod>) = items.all { it.images.isNotEmpty() }

    /** Deletions of its pods; a success shows the pod terminating, and soon its replacement. */
    val deletions = PodDeletions(viewModelScope, kube) { refresh() }
}

/**
 * Pod deletions in [scope] (a ViewModel's): those in flight ([deleting]) and their outcome,
 * shown once ([results]). [onDeleted] runs after a success.
 */
class PodDeletions(private val scope: CoroutineScope, private val kube: KubeRepository, private val onDeleted: () -> Unit) {
    private val _deleting = MutableStateFlow<Set<String>>(emptySet())
    /** Keys of the pods whose deletion is in flight. */
    val deleting: StateFlow<Set<String>> = _deleting.asStateFlow()

    private val _results = Channel<DeleteResult>(Channel.BUFFERED)
    val results: Flow<DeleteResult> = _results.receiveAsFlow()

    fun delete(pod: KubePod) {
        if (pod.key in _deleting.value) return
        _deleting.update { it + pod.key }
        scope.launch {
            val outcome = cancellableCatching { kube.deletePod(pod) }
            _deleting.update { it - pod.key }
            _results.send(DeleteResult(pod, outcome.exceptionOrNull()?.uiText()))
            if (outcome.isSuccess) onDeleted()
        }
    }
}

/** The pod a row asked to delete (confirmed first) or to read the logs of. */
@Stable
class PodActionState {
    var confirm by mutableStateOf<KubePod?>(null)
    var logs by mutableStateOf<KubePod?>(null)
}

/** The logs sheet, the delete confirmation and a toast per deletion outcome of a pod list. */
@Composable
internal fun PodActionDialogs(actions: PodActionState, deletions: PodDeletions) {
    actions.logs?.let { PodLogSheet(it, onDismiss = { actions.logs = null }) }

    val context = LocalContext.current
    LaunchedEffect(deletions) {
        deletions.results.collect { r ->
            val text = r.error?.resolve(context)?.let { context.getString(R.string.pods_delete_failed, r.pod.name, it) }
                ?: context.getString(R.string.pods_delete_done, r.pod.name)
            Toast.makeText(context, text, if (r.error == null) Toast.LENGTH_SHORT else Toast.LENGTH_LONG).show()
        }
    }

    actions.confirm?.let { pod ->
        DeleteConfirmDialog(
            pod = pod,
            onConfirm = {
                actions.confirm = null
                deletions.delete(pod)
            },
            onDismiss = { actions.confirm = null },
        )
    }
}

/**
 * The pods of the scope's namespace (every namespace by default) with the status `kubectl get
 * pods` shows, unhealthy ones first once every page is loaded, their logs, and a delete action
 * so a controller starts a fresh one. [focusKey]: the pod whose logs to open once listed (a
 * share link), then [onFocused].
 */
@Composable
fun PodsTab(
    control: KubeScopeControl,
    query: String,
    onQuery: (String) -> Unit,
    modifier: Modifier = Modifier,
    onFlows: ((KubePod) -> Unit)? = null,
    vm: PodsViewModel = viewModel(factory = factory { PodsViewModel(app.kubeRepository) { isMeteredNetwork(app) } }),
    focusKey: String? = null,
    onFocused: () -> Unit = {},
) {
    val state by vm.state.collectAsStateWithLifecycle()
    val progress by vm.progress.collectAsStateWithLifecycle()
    val deleting by vm.deletions.deleting.collectAsStateWithLifecycle()
    LaunchedEffect(control.scope, control.ready) { if (control.ready) vm.setScope(control.scope) }
    val actions = remember { PodActionState() }
    PodActionDialogs(actions, vm.deletions)
    val listed = (state as? UiState.Loaded)?.data?.items
    LaunchedEffect(focusKey, listed) {
        listed.orEmpty().firstOrNull { it.key == focusKey }?.let {
            actions.logs = it
            onFocused()
        }
    }

    KubeListFrame(control, state, { it.podNamespaces }, query, onQuery, vm::refresh, modifier) { s ->
        val load = s.data
        val selected = control.scope.namespace
        // Sorted once complete; image search only when every row carries its images.
        val rows = remember(load, selected, query) { load.items.filteredPods(selected, query, sorted = load.done, searchImages = load.detailed) }
        PagedProgress(progress)
        KubeDenialNote(rememberKubeDenial(KubeAction.DELETE_POD, selected.orEmpty()), Modifier.padding(horizontal = 16.dp, vertical = 4.dp))
        IncompleteNotice(load, searching = query.isNotBlank(), onLoadMore = vm::loadMore, onLoadAll = vm::loadAll)
        PullToRefreshBox(isRefreshing = s.refreshing, onRefresh = vm::refresh, modifier = Modifier.weight(1f)) {
            if (rows.isEmpty()) {
                EmptyText(emptyOrNoMatch(query, R.string.pods_empty, R.string.pods_no_match))
            } else {
                val listState = rememberLazyListState()
                LoadMoreOnScroll(listState, enabled = load.hasMore && query.isBlank(), loaded = load.items.size, onLoadMore = vm::loadMore)
                LazyColumn(Modifier.fillMaxSize(), state = listState) {
                    items(rows, key = { it.key }) { pod ->
                        PodRow(
                            pod,
                            showNamespace = selected == null,
                            deleting = pod.key in deleting,
                            onDelete = { actions.confirm = pod },
                            onLogs = { actions.logs = pod },
                            onFlows = onFlows?.let { open -> { open(pod) } },
                        )
                        HorizontalDivider()
                    }
                }
            }
        }
        DataFreshness(s, edgeToEdge = false)
    }
}

/** A pod: name, namespace and node (when asked), status, readiness, restarts; logs, flows and delete actions. */
@Composable
internal fun PodRow(
    pod: KubePod,
    showNamespace: Boolean,
    deleting: Boolean,
    onDelete: () -> Unit,
    onLogs: () -> Unit,
    onFlows: (() -> Unit)? = null,
    showNode: Boolean = true,
) {
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    Row(Modifier.fillMaxWidth().padding(start = 16.dp, end = 4.dp, top = 6.dp, bottom = 6.dp), verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f)) {
            Text(pod.name, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Text(
                listOfNotNull(pod.namespace.takeIf { showNamespace }, pod.node.takeIf { showNode && it.isNotEmpty() }).joinToString("  ·  "),
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
        IconButton(onClick = onLogs) {
            Icon(Icons.AutoMirrored.Outlined.Article, stringResource(R.string.pod_logs_open, pod.name))
        }
        if (onFlows != null) {
            IconButton(onClick = onFlows) {
                Icon(Icons.Outlined.Stream, stringResource(R.string.pods_live_flows, pod.name))
            }
        }
        if (deleting) {
            CircularProgressIndicator(Modifier.padding(12.dp).size(24.dp), strokeWidth = 2.dp)
        } else {
            val denied = rememberKubeDenial(KubeAction.DELETE_POD, pod.namespace) != null
            IconButton(onClick = onDelete, enabled = pod.status != "Terminating" && !denied) {
                Icon(Icons.Outlined.Delete, stringResource(R.string.pods_delete, pod.name))
            }
        }
    }
}

@Composable
private fun DeleteConfirmDialog(pod: KubePod, onConfirm: () -> Unit, onDismiss: () -> Unit) {
    ConfirmDialog(
        title = stringResource(R.string.pods_delete_title, pod.name),
        text = if (pod.owner.isNotEmpty()) {
            stringResource(R.string.pods_delete_text_owned, pod.namespace, pod.owner)
        } else {
            stringResource(R.string.pods_delete_text_bare, pod.namespace)
        },
        confirm = stringResource(R.string.pods_delete_confirm),
        onConfirm = onConfirm,
        onDismiss = onDismiss,
        destructive = true,
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
