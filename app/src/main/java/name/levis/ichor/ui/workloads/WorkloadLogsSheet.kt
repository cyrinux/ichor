package name.levis.ichor.ui.workloads

import android.widget.Toast
import androidx.compose.foundation.background
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
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material.icons.outlined.Share
import androidx.compose.material3.ExperimentalMaterial3Api
import name.levis.ichor.ui.design.IchorFilterChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.compose.viewModel
import java.text.NumberFormat
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.KubeRepository
import name.levis.ichor.ui.design.IchorAssistChip
import name.levis.ichor.ui.kubebrowser.LocalKubeLinks
import name.levis.ichor.ui.logsearch.openInLogsBackendLabel
import name.levis.ichor.ui.logsearch.rememberLogsBackend
import name.levis.ichor.model.KubeWorkload
import name.levis.ichor.model.LogFilter
import name.levis.ichor.model.WORKLOAD_LOG_COLORS
import name.levis.ichor.model.WORKLOAD_LOG_TAILS
import name.levis.ichor.model.WorkloadLogLine
import name.levis.ichor.model.WorkloadLogs
import name.levis.ichor.model.asText
import name.levis.ichor.model.podSuffix
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.cancellableCatching
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SkeletonStyle
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.copyToClipboard
import name.levis.ichor.ui.components.upwardScrollStaysInSheet
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.logs.rememberLogPalette
import name.levis.ichor.ui.uiText

/** What the sheet reads: the filter, and the lines per pod. */
data class WorkloadLogQuery(val filter: LogFilter = LogFilter(), val tail: Int = WORKLOAD_LOG_TAILS.last())

/** The merged log of the workload the sheet shows; reads it again for each [load]. */
class WorkloadLogsViewModel(private val kube: KubeRepository) : ViewModel() {
    private val _query = MutableStateFlow(WorkloadLogQuery())
    val query: StateFlow<WorkloadLogQuery> = _query.asStateFlow()

    private val _state = MutableStateFlow<UiState<WorkloadLogs>>(UiState.Loading)
    val state: StateFlow<UiState<WorkloadLogs>> = _state.asStateFlow()
    private var job: Job? = null

    fun load(workload: KubeWorkload, query: WorkloadLogQuery = _query.value) {
        job?.cancel()
        _query.value = query
        _state.value = UiState.Loading
        job = viewModelScope.launch {
            val outcome = cancellableCatching { kube.workloadLogs(workload, query.filter, query.tail) }
            _state.value = outcome.fold(onSuccess = { UiState.Loaded(it) }, onFailure = { UiState.Failed(it.uiText()) })
        }
    }

    /** Forgets the log (it can be large) once the sheet is gone. */
    fun close() {
        job?.cancel()
        _state.value = UiState.Loading
        _query.value = WorkloadLogQuery()
    }
}

/**
 * The logs of [workload]'s pods merged by time, like `kubectl logs -l`, a chip per pod, with a
 * filter, a time range, copy and share.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun WorkloadLogsSheet(
    workload: KubeWorkload,
    onDismiss: () -> Unit,
    vm: WorkloadLogsViewModel = viewModel(key = "workload-logs", factory = factory { WorkloadLogsViewModel(app.kubeRepository) }),
) {
    val context = LocalContext.current
    val links = LocalKubeLinks.current
    val appContext = context.applicationContext as TalosApp
    val logsBackend = rememberLogsBackend()
    val state by vm.state.collectAsStateWithLifecycle()
    val query by vm.query.collectAsStateWithLifecycle()
    val logs = (state as? UiState.Loaded)?.data
    val hasText = !logs?.lines.isNullOrEmpty()
    LaunchedEffect(workload.key) { vm.load(workload) }
    DisposableEffect(vm) {
        onDispose {
            vm.close()
            deleteSharedLogs(context)
        }
    }
    ModalBottomSheet(onDismissRequest = onDismiss, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)) {
        Column(Modifier.fillMaxHeight(0.92f).padding(bottom = 16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Row(Modifier.padding(start = 16.dp, end = 4.dp)) {
                Column(Modifier.weight(1f)) {
                    Text(workload.name, style = MaterialTheme.typography.titleMedium, fontFamily = FontFamily.Monospace, maxLines = 2, overflow = TextOverflow.Ellipsis)
                    Text("${workload.kind}  ·  ${workload.namespace}", style = MaterialTheme.typography.labelSmall, fontFamily = FontFamily.Monospace, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
                TooltipIconButton(Icons.Outlined.ContentCopy, stringResource(R.string.pod_logs_copy), enabled = hasText, onClick = {
                    // Logs may hold secrets: kept out of the clipboard preview (honoured from Android 13).
                    copyToClipboard(context, workload.name, logs?.asText().orEmpty(), sensitive = true)
                    Toast.makeText(context, R.string.pod_logs_copied, Toast.LENGTH_SHORT).show()
                })
                TooltipIconButton(Icons.Outlined.Share, stringResource(R.string.pod_logs_share), enabled = hasText, onClick = {
                    shareLog(context, workload.name, logs?.asText().orEmpty())
                })
            }
            LogFilterBar(query.filter, { vm.load(workload, query.copy(filter = it)) })
            LazyRow(contentPadding = PaddingValues(horizontal = 16.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                items(WORKLOAD_LOG_TAILS, key = { "tail-$it" }) { tail ->
                    IchorFilterChip(
                        selected = query.tail == tail,
                        onClick = { vm.load(workload, query.copy(tail = tail)) },
                        label = { Text(stringResource(R.string.workload_logs_tail, tail)) },
                    )
                }
                if (links != null && logsBackend != null) {
                    item(key = "log-backend") {
                        IchorAssistChip(onClick = {
                            onDismiss()
                            links.onLogSearch(appContext.logsBackendRepository.workloadQuery(logsBackend.kind, workload.namespace, workload.name))
                        }, label = { Text(openInLogsBackendLabel(logsBackend)) })
                    }
                }
            }
            Box(Modifier.weight(1f).upwardScrollStaysInSheet()) {
                when (val s = state) {
                    UiState.Loading -> LoadingBox(style = SkeletonStyle.TEXT)
                    is UiState.Failed -> ErrorBox(s.message, { vm.load(workload) })
                    is UiState.Loaded -> MergedLog(s.data)
                }
            }
            if (logs != null) LogFooter(logs, Modifier.padding(horizontal = 16.dp))
        }
    }
}

@Composable
private fun MergedLog(logs: WorkloadLogs) {
    if (logs.lines.isEmpty()) {
        EmptyText(stringResource(R.string.pod_logs_empty))
        return
    }
    val colors = remember(logs.pods) { logs.pods.associate { it.name to it.color } }
    val list = rememberLazyListState()
    // The newest lines are at the end, as in a terminal.
    LaunchedEffect(logs) { list.scrollToItem(logs.lines.lastIndex) }
    LazyColumn(Modifier.fillMaxWidth(), state = list, contentPadding = PaddingValues(horizontal = 16.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
        items(logs.lines) { line -> MergedLogLine(line, colors[line.pod] ?: 0) }
    }
}

@Composable
private fun MergedLogLine(line: WorkloadLogLine, color: Int) {
    val palette = rememberLogPalette()
    val podLabel = stringResource(R.string.workload_logs_pod, line.pod)
    val textColor = when (line.level) {
        "error" -> palette.error
        "warn", "warning" -> palette.warn
        else -> palette.text
    }
    Row(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalAlignment = Alignment.Top) {
        Text(
            line.podSuffix,
            style = MaterialTheme.typography.labelSmall,
            fontFamily = FontFamily.Monospace,
            color = MaterialTheme.colorScheme.onSurface,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
            modifier = Modifier
                .semantics { contentDescription = podLabel }
                .background(PodColors[color.mod(WORKLOAD_LOG_COLORS)].copy(alpha = 0.35f), RoundedCornerShape(4.dp))
                .padding(horizontal = 4.dp),
        )
        if (line.time > 0) {
            Text(TimeFormat.format(Instant.ofEpochMilli(line.time)), style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace, color = palette.dim)
        }
        Text(line.text, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace, color = textColor)
    }
}

/** "Matched 42 of 3,120 · truncated". */
@Composable
private fun LogFooter(logs: WorkloadLogs, modifier: Modifier = Modifier) {
    val numbers = remember { NumberFormat.getIntegerInstance() }
    val matched = stringResource(R.string.workload_logs_matched, numbers.format(logs.matched), numbers.format(logs.scanned))
    MutedText(if (logs.truncated) "$matched · ${stringResource(R.string.workload_logs_truncated)}" else matched, modifier)
}

private val TimeFormat: DateTimeFormatter = DateTimeFormatter.ofPattern("HH:mm:ss").withZone(ZoneId.systemDefault())

/** The pods' colours, tinting a chip under the theme's text: one per [WorkloadLogPod.color]. */
private val PodColors = listOf(
    Color(0xFF2A78D6),
    Color(0xFFE8833A),
    Color(0xFF2FA36B),
    Color(0xFFB25FD3),
    Color(0xFFD64F6A),
    Color(0xFF17A2B8),
    Color(0xFFC9A227),
    Color(0xFF7A869A),
)
