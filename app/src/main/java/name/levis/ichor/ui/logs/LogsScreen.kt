package name.levis.ichor.ui.logs

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.outlined.Check
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material3.Checkbox
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.repeatOnLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.produceIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.withContext
import name.levis.ichor.R
import name.levis.ichor.data.StreamItem
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.model.FOLLOW_TAIL_LINES
import name.levis.ichor.model.LogLevelFilter
import name.levis.ichor.model.LogSource
import name.levis.ichor.model.LogTail
import name.levis.ichor.model.TalosFeature
import name.levis.ichor.model.support
import name.levis.ichor.ui.components.rememberNodeFeatures
import androidx.compose.ui.text.style.TextOverflow
import name.levis.ichor.model.SeqLogEntry
import name.levis.ichor.model.appendCapped
import name.levis.ichor.model.numbered
import name.levis.ichor.model.toEntries
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.DataFreshness
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.LiveIndicator
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.userMessage

class LogsViewModel(
    private val talos: TalosRepository,
    private val node: String,
    private val source: LogSource,
) : LoadingViewModel<LogTail>() {
    override suspend fun fetch() = when (source) {
        is LogSource.Service -> talos.logs(node, source.name)
        is LogSource.Container -> talos.containerLogs(node, source.id)
    }
}

data class FollowState(
    val entries: List<SeqLogEntry> = emptyList(),
    val streaming: Boolean = false,
    val error: String? = null,
    /** Number of the next entry, so rows keep their keys as old ones are dropped. */
    val nextSeq: Long = 0,
)

/** Pause between two applied batches of followed lines, so bursts redraw a few times only. */
private const val FOLLOW_BATCH_MS = 100L

/** Follows the log (`talosctl logs -f`) while [follow] is collected. */
class LogFollowViewModel(
    private val talos: TalosRepository,
    private val node: String,
    private val source: LogSource,
) : ViewModel() {
    private val _state = MutableStateFlow(FollowState())
    val state: StateFlow<FollowState> = _state.asStateFlow()

    /** Starts over with the last FOLLOW_TAIL_LINES lines; runs until cancelled or the stream ends. */
    suspend fun follow() {
        _state.value = FollowState(streaming = true)
        try {
            coroutineScope {
                val lines = when (source) {
                    is LogSource.Service -> talos.followLogs(node, source.name, FOLLOW_TAIL_LINES)
                    is LogSource.Container -> talos.followContainerLogs(node, source.id, FOLLOW_TAIL_LINES)
                }
                val items = lines.produceIn(this)
                while (true) {
                    val next = items.receiveCatching()
                    next.exceptionOrNull()?.let { throw it }
                    val first = next.getOrNull() ?: break
                    // Everything already queued joins the batch.
                    val batch = buildList {
                        add(first)
                        while (true) add(items.tryReceive().getOrNull() ?: break)
                    }
                    apply(batch)
                    delay(FOLLOW_BATCH_MS)
                }
            }
        } catch (e: CancellationException) {
            throw e
        } catch (e: Throwable) {
            _state.update { it.copy(error = e.userMessage()) }
        } finally {
            _state.update { it.copy(streaming = false) }
        }
    }

    private suspend fun apply(batch: List<StreamItem<String>>) {
        val lines = batch.filterIsInstance<StreamItem.Item<String>>().map { it.value }
        val parsed = withContext(Dispatchers.IO) { lines.map(talos::parseLogLine) }
        val done = batch.filterIsInstance<StreamItem.Done>().lastOrNull()
        _state.update {
            it.copy(
                entries = it.entries.appendCapped(parsed.numbered(it.nextSeq)),
                nextSeq = it.nextSeq + parsed.size,
                streaming = it.streaming && done == null,
                error = done?.error ?: it.error,
            )
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun LogsScreen(
    node: String,
    hostname: String,
    source: LogSource,
    onBack: () -> Unit,
    vm: LogsViewModel = viewModel(
        key = "logs-$node-${source.key}",
        factory = factory { LogsViewModel(app.talosRepository, node, source) },
    ),
    followVm: LogFollowViewModel = viewModel(
        key = "logfollow-$node-${source.key}",
        factory = factory { LogFollowViewModel(app.talosRepository, node, source) },
    ),
) {
    val canFollow = rememberNodeFeatures(node).support(TalosFeature.LOG_FOLLOW).supported
    val state by vm.state.collectAsStateWithLifecycle()
    val followState by followVm.state.collectAsStateWithLifecycle()
    var filter by rememberSaveable { mutableStateOf("") }
    var follow by rememberSaveable { mutableStateOf(false) }
    var raw by rememberSaveable { mutableStateOf(false) }
    var level by rememberSaveable { mutableStateOf(LogLevelFilter.ALL) }
    var menuOpen by remember { mutableStateOf(false) }
    val lifecycle = LocalLifecycleOwner.current.lifecycle
    LaunchedEffect(follow) { if (!follow && state == UiState.Loading) vm.refresh() }
    // Stream only while following and visible: toggling off, leaving or backgrounding cancels it.
    LaunchedEffect(follow, lifecycle) {
        if (follow) lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) { followVm.follow() }
    }

    Scaffold(
        bottomBar = {
            if (follow) {
                Surface(color = MaterialTheme.colorScheme.surfaceContainer, modifier = Modifier.fillMaxWidth()) {
                    LiveIndicator(
                        followState.streaming,
                        followState.error,
                        Modifier.navigationBarsPadding().padding(horizontal = 16.dp, vertical = 6.dp),
                    )
                }
            } else {
                DataFreshness(state)
            }
        },
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        when (source) {
                            is LogSource.Service -> {
                                Text(source.name ?: stringResource(R.string.logs_kernel_log))
                                Text(hostname, style = MaterialTheme.typography.labelMedium)
                            }
                            is LogSource.Container -> {
                                Text(source.title, maxLines = 1, overflow = TextOverflow.Ellipsis)
                                Text(
                                    source.subtitle.ifEmpty { hostname },
                                    style = MaterialTheme.typography.labelMedium,
                                    maxLines = 1,
                                    overflow = TextOverflow.Ellipsis,
                                )
                            }
                        }
                    }
                },
                navigationIcon = { IconButton(onClick = onBack) { Icon(Icons.AutoMirrored.Filled.ArrowBack, stringResource(R.string.common_back)) } },
                actions = {
                    FilterChip(
                        selected = follow,
                        enabled = canFollow || follow,
                        onClick = {
                            follow = !follow
                            if (!follow) vm.refresh()
                        },
                        label = { Text(stringResource(R.string.logs_follow)) },
                        leadingIcon = if (follow) ({ Icon(Icons.Outlined.Check, contentDescription = null, Modifier.size(18.dp)) }) else null,
                    )
                    if (!follow) IconButton(onClick = vm::refresh) { Icon(Icons.Outlined.Refresh, stringResource(R.string.common_refresh)) }
                    Box {
                        IconButton(onClick = { menuOpen = true }) { Icon(Icons.Outlined.MoreVert, stringResource(R.string.common_more)) }
                        DropdownMenu(expanded = menuOpen, onDismissRequest = { menuOpen = false }) {
                            DropdownMenuItem(
                                text = { Text(stringResource(R.string.logs_raw)) },
                                trailingIcon = { Checkbox(checked = raw, onCheckedChange = null) },
                                onClick = {
                                    raw = !raw
                                    menuOpen = false
                                },
                            )
                        }
                    }
                },
            )
        },
    ) { padding ->
        Column(Modifier.padding(padding).fillMaxSize()) {
            OutlinedTextField(
                value = filter,
                onValueChange = { filter = it },
                placeholder = { Text(stringResource(R.string.logs_filter)) },
                leadingIcon = { Icon(Icons.Outlined.Search, contentDescription = null) },
                singleLine = true,
                modifier = Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 4.dp),
            )
            val view = LogView(filter, level, raw, onLevel = { level = it })
            if (follow) {
                LogContent(followState.entries, truncated = false, view, live = true)
            } else {
                when (val s = state) {
                    UiState.Loading -> LoadingBox()
                    is UiState.Failed -> ErrorBox(s.message, vm::refresh)
                    is UiState.Loaded -> {
                        val entries = remember(s.data) { s.data.toEntries().numbered() }
                        LogContent(entries, s.data.truncated, view, live = false)
                    }
                }
            }
        }
    }
}
