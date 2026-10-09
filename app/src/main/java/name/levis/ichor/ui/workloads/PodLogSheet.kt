package name.levis.ichor.ui.workloads

import android.content.Context
import android.widget.Toast
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material.icons.outlined.Link
import androidx.compose.material.icons.outlined.Share
import androidx.compose.material3.AssistChip
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.repeatOnLifecycle
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.compose.viewModel
import java.io.File
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.data.KubeRepository
import name.levis.ichor.model.KubeObjectRef
import name.levis.ichor.model.KubePod
import name.levis.ichor.model.POD_LOG_TAIL
import name.levis.ichor.model.containersToChoose
import name.levis.ichor.model.logLines
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.cancellableCatching
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.InfoNotice
import name.levis.ichor.ui.components.LiveIndicator
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.SkeletonStyle
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.upwardScrollStaysInSheet
import name.levis.ichor.model.ShareTarget
import name.levis.ichor.ui.components.copyToClipboard
import name.levis.ichor.ui.share.ShareLinkButton
import name.levis.ichor.ui.components.shareFile
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.kubebrowser.LocalKubeLinks
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.uiText

/** What the log sheet shows: the current or [previous] run of [container] ("" for the only one). */
data class PodLogQuery(val container: String = "", val previous: Boolean = false)

/**
 * The log of the pod the sheet shows ([open]), through the Kubernetes API; for a pod with
 * several containers, the one picked. One for every pod: [close] drops the text.
 */
class PodLogViewModel(private val kube: KubeRepository) : ViewModel() {
    private var pod: KubePod? = null

    private val _query = MutableStateFlow(PodLogQuery())
    val query: StateFlow<PodLogQuery> = _query.asStateFlow()

    private val _containers = MutableStateFlow<List<String>>(emptyList())
    /** The containers to pick from; empty for a pod with one container. */
    val containers: StateFlow<List<String>> = _containers.asStateFlow()

    private val _detail = MutableStateFlow<KubePod?>(null)
    /**
     * The pod with its containers and last termination: read in full when the list's row
     * lacks them (a Table row of a large cluster); the row itself if that fails.
     */
    val detail: StateFlow<KubePod?> = _detail.asStateFlow()

    private val _state = MutableStateFlow<UiState<String>>(UiState.Loading)
    val state: StateFlow<UiState<String>> = _state.asStateFlow()
    private var job: Job? = null

    /** Shows [pod]'s current log; with several containers, the first one until another is picked. */
    fun open(pod: KubePod) {
        job?.cancel()
        this.pod = pod
        _detail.value = pod
        if (pod.containerNames.isNotEmpty()) return start(pod)
        _state.value = UiState.Loading
        job = viewModelScope.launch {
            val full = cancellableCatching { kube.pod(pod.namespace, pod.name) }.getOrNull() ?: pod
            this@PodLogViewModel.pod = full
            _detail.value = full
            start(full)
        }
    }

    private fun start(pod: KubePod) {
        val names = pod.containerNames.takeIf { it.size > 1 }.orEmpty()
        _containers.value = names
        load(PodLogQuery(container = names.firstOrNull().orEmpty()))
    }

    /** Forgets the log (it can be large) once the sheet is gone. */
    fun close() {
        job?.cancel()
        pod = null
        _detail.value = null
        _state.value = UiState.Loading
        _containers.value = emptyList()
        _query.value = PodLogQuery()
    }

    fun load(query: PodLogQuery = _query.value) {
        val pod = pod ?: return
        job?.cancel()
        _query.value = query
        _state.value = UiState.Loading
        job = viewModelScope.launch {
            val outcome = cancellableCatching { kube.podLogs(pod, query.container, query.previous, POD_LOG_TAIL) }
            val choices = outcome.exceptionOrNull()?.message?.let(::containersToChoose).orEmpty()
            // Several containers the pod list did not name (older core): the API names them.
            if (query.container.isEmpty() && choices.isNotEmpty()) {
                _containers.value = choices
                load(query.copy(container = choices.first()))
                return@launch
            }
            _state.value = outcome.fold(onSuccess = { UiState.Loaded(it) }, onFailure = { UiState.Failed(it.uiText()) })
        }
    }
}

/**
 * The log of [pod] like `kubectl logs`, the last lines, with copy and share. "Previous run"
 * (the container's last terminated run, which Talos no longer keeps) is offered when the pod
 * restarted.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun PodLogSheet(
    pod: KubePod,
    onDismiss: () -> Unit,
    vm: PodLogViewModel = viewModel(key = "pod-log", factory = factory { PodLogViewModel(app.kubeRepository) }),
    followVm: PodLogFollowViewModel = viewModel(key = "pod-log-follow", factory = factory { PodLogFollowViewModel(app.kubeBrowser) }),
) {
    val context = LocalContext.current
    val links = LocalKubeLinks.current
    val state by vm.state.collectAsStateWithLifecycle()
    val followState by followVm.state.collectAsStateWithLifecycle()
    val query by vm.query.collectAsStateWithLifecycle()
    val containers by vm.containers.collectAsStateWithLifecycle()
    val detail by vm.detail.collectAsStateWithLifecycle()
    val lastTermination = detail?.lastTermination.orEmpty()
    // The pod's events in place of its log: why it does not start, when there is no log yet.
    var events by rememberSaveable(pod.key) { mutableStateOf(false) }
    // New lines as they are written (`kubectl logs -f`), in place of the last ones read once.
    var follow by rememberSaveable(pod.key) { mutableStateOf(false) }
    val snapshot = (state as? UiState.Loaded)?.data
    val hasText = if (follow) followState.buffer.lines.isNotEmpty() else !snapshot.isNullOrEmpty()
    // The followed lines joined only when asked: up to MAX_FOLLOW_LINES of them.
    val text = { if (follow) followState.buffer.text else snapshot.orEmpty() }
    LaunchedEffect(pod.key) { vm.open(pod) }
    DisposableEffect(vm) {
        onDispose {
            vm.close()
            followVm.clear()
            deleteSharedLogs(context)
        }
    }
    // Stream only while following and visible: toggling off, closing or backgrounding cancels it.
    val lifecycle = LocalLifecycleOwner.current.lifecycle
    LaunchedEffect(follow, query.container, lifecycle) {
        if (follow) lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) { followVm.follow(pod.namespace, pod.name, query.container) }
    }

    ModalBottomSheet(onDismissRequest = onDismiss, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)) {
        Column(Modifier.fillMaxHeight(0.92f).padding(bottom = 16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Row(Modifier.padding(start = 16.dp, end = 4.dp)) {
                Column(Modifier.weight(1f)) {
                    Text(pod.name, style = MaterialTheme.typography.titleMedium, fontFamily = FontFamily.Monospace, maxLines = 2, overflow = TextOverflow.Ellipsis)
                    Text(pod.namespace, style = MaterialTheme.typography.labelSmall, fontFamily = FontFamily.Monospace, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
                ShareLinkButton(ShareTarget.pod(pod.namespace, pod.name), icon = Icons.Outlined.Link)
                TooltipIconButton(Icons.Outlined.ContentCopy, stringResource(R.string.pod_logs_copy), enabled = hasText, onClick = {
                    copyLog(context, pod, text())
                    Toast.makeText(context, R.string.pod_logs_copied, Toast.LENGTH_SHORT).show()
                })
                TooltipIconButton(Icons.Outlined.Share, stringResource(R.string.pod_logs_share), enabled = hasText, onClick = {
                    shareLog(context, pod, text())
                })
            }
            run {
                LazyRow(contentPadding = PaddingValues(horizontal = 16.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    items(containers, key = { "c-$it" }) { c ->
                        FilterChip(
                            selected = query.container == c,
                            onClick = { vm.load(query.copy(container = c)) },
                            label = { Text(c, fontFamily = FontFamily.Monospace) },
                        )
                    }
                    if (!query.previous) {
                        item(key = "follow") {
                            FilterChip(
                                selected = follow,
                                enabled = follow || state is UiState.Loaded,
                                onClick = {
                                    follow = !follow
                                    if (!follow) vm.load()
                                },
                                label = { Text(stringResource(R.string.logs_follow)) },
                            )
                        }
                    }
                    if (pod.restarts > 0) {
                        item(key = "previous") {
                            FilterChip(
                                selected = query.previous,
                                onClick = {
                                    follow = false
                                    vm.load(query.copy(previous = !query.previous))
                                },
                                label = { Text(stringResource(R.string.pod_logs_previous)) },
                            )
                        }
                    }
                    item(key = "events") {
                        FilterChip(selected = events, onClick = { events = !events }, label = { Text(stringResource(R.string.kube_events_title)) })
                    }
                    if (links != null) {
                        item(key = "yaml") {
                            AssistChip(onClick = {
                                onDismiss()
                                links.onObject(KubeObjectRef.pod(pod.namespace, pod.name))
                            }, label = { Text(stringResource(R.string.kb_tab_yaml)) })
                        }
                        item(key = "forward") {
                            AssistChip(onClick = {
                                onDismiss()
                                links.onPortForward(pod.namespace, pod.name)
                            }, label = { Text(stringResource(R.string.kb_forward_title)) })
                        }
                        item(key = "shell") {
                            // In the container picked for the log; "" lets Kubernetes pick the only one.
                            AssistChip(onClick = {
                                onDismiss()
                                links.onShell(pod.namespace, pod.name, query.container)
                            }, label = { Text(stringResource(R.string.pod_shell_title)) })
                        }
                    }
                }
            }
            if (lastTermination.isNotEmpty()) {
                Text(
                    stringResource(R.string.pod_logs_last_termination, lastTermination),
                    style = MaterialTheme.typography.bodySmall,
                    color = LocalStatusColors.current.warn,
                    modifier = Modifier.padding(horizontal = 16.dp),
                )
            }
            if (follow && !events) {
                LiveIndicator(followState.streaming, followState.error, Modifier.padding(horizontal = 16.dp))
            } else {
                InfoNotice(stringResource(R.string.pod_logs_tail, POD_LOG_TAIL), Modifier.padding(horizontal = 16.dp))
            }
            Box(Modifier.weight(1f).upwardScrollStaysInSheet()) {
                if (events) {
                    KubeEventsList(pod.namespace, "Pod", pod.name, Modifier.verticalScroll(rememberScrollState()).padding(horizontal = 16.dp))
                } else if (follow) {
                    FollowedLog(followState.buffer)
                } else {
                    when (val s = state) {
                        UiState.Loading -> LoadingBox(style = SkeletonStyle.TEXT)
                        is UiState.Failed -> ErrorBox(s.message, { vm.load() })
                        is UiState.Loaded -> LogText(s.data)
                    }
                }
            }
        }
    }
}

@Composable
private fun LogText(text: String) {
    val lines = logLines(text)
    if (lines.isEmpty()) {
        EmptyText(stringResource(R.string.pod_logs_empty))
        return
    }
    val list = rememberLazyListState()
    // The newest lines are at the end, as in a terminal.
    LaunchedEffect(text) { list.scrollToItem(lines.lastIndex) }
    LazyColumn(Modifier.fillMaxWidth(), state = list, contentPadding = PaddingValues(horizontal = 16.dp)) {
        items(lines.size) { i ->
            Text(lines[i], style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace)
        }
    }
}

// Logs may hold secrets: kept out of the clipboard preview (honoured from Android 13).
private fun copyLog(context: Context, pod: KubePod, text: String) = copyToClipboard(context, pod.name, text, sensitive = true)

private fun sharedLogs(context: Context) = File(context.cacheDir, "logs")

/** Removes the shared log file, once the sheet closes. */
private fun deleteSharedLogs(context: Context) {
    sharedLogs(context).listFiles()?.forEach { it.delete() }
}

/** Shares the log as a file in the cache: it can be larger than an intent can carry as text. One file, replaced each time. */
private fun shareLog(context: Context, pod: KubePod, text: String) {
    deleteSharedLogs(context)
    val dir = sharedLogs(context).apply { mkdirs() }
    val file = File(dir, "${pod.name}.log")
    try {
        file.writeText(text)
    } catch (e: java.io.IOException) {
        Toast.makeText(context, e.message.orEmpty(), Toast.LENGTH_LONG).show()
        return
    }
    shareFile(context, file, "text/plain", R.string.pod_logs_share_chooser)
}
