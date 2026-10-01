package name.levis.talosmobile.ui.logs

import androidx.compose.ui.res.stringResource
import name.levis.talosmobile.R
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.outlined.ArrowDownward
import androidx.compose.material.icons.outlined.Check
import androidx.compose.material3.ExtendedFloatingActionButton
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Surface
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.Alignment
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.repeatOnLifecycle
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import name.levis.talosmobile.data.StreamItem
import name.levis.talosmobile.model.FOLLOW_TAIL_LINES
import name.levis.talosmobile.model.LogTail
import name.levis.talosmobile.model.appendCapped
import name.levis.talosmobile.model.matching
import name.levis.talosmobile.ui.components.LiveIndicator
import name.levis.talosmobile.ui.userMessage
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
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
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.talosmobile.data.TalosRepository
import name.levis.talosmobile.ui.LoadingViewModel
import name.levis.talosmobile.ui.UiState
import name.levis.talosmobile.ui.app
import name.levis.talosmobile.ui.components.DataFreshness
import name.levis.talosmobile.ui.components.ErrorBox
import name.levis.talosmobile.ui.components.LoadingBox
import name.levis.talosmobile.ui.factory

/** [service] null means the kernel log (dmesg). */
class LogsViewModel(
    private val talos: TalosRepository,
    private val node: String,
    private val service: String?,
) : LoadingViewModel<LogTail>() {
    override suspend fun fetch() = talos.logs(node, service)
}

data class FollowState(val lines: List<String> = emptyList(), val streaming: Boolean = false, val error: String? = null)

/** Follows the log (`talosctl logs -f`) while [follow] is collected. */
class LogFollowViewModel(
    private val talos: TalosRepository,
    private val node: String,
    private val service: String?,
) : ViewModel() {
    private val _state = MutableStateFlow(FollowState())
    val state: StateFlow<FollowState> = _state.asStateFlow()

    /** Starts over with the last FOLLOW_TAIL_LINES lines; runs until cancelled or the stream ends. */
    suspend fun follow() {
        _state.value = FollowState(streaming = true)
        try {
            talos.followLogs(node, service, FOLLOW_TAIL_LINES).collect { item ->
                when (item) {
                    is StreamItem.Item -> _state.update { it.copy(lines = it.lines.appendCapped(listOf(item.value))) }
                    is StreamItem.Done -> _state.update { it.copy(streaming = false, error = item.error) }
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
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun LogsScreen(
    node: String,
    hostname: String,
    service: String?,
    onBack: () -> Unit,
    vm: LogsViewModel = viewModel(
        key = "logs-$node-${service ?: "kernel"}",
        factory = factory { LogsViewModel(app.talosRepository, node, service) },
    ),
    followVm: LogFollowViewModel = viewModel(
        key = "logfollow-$node-${service ?: "kernel"}",
        factory = factory { LogFollowViewModel(app.talosRepository, node, service) },
    ),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    val followState by followVm.state.collectAsStateWithLifecycle()
    var filter by rememberSaveable { mutableStateOf("") }
    var follow by rememberSaveable { mutableStateOf(false) }
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
                        Text(service ?: stringResource(R.string.logs_kernel_log))
                        Text(hostname, style = MaterialTheme.typography.labelMedium)
                    }
                },
                navigationIcon = { IconButton(onClick = onBack) { Icon(Icons.AutoMirrored.Filled.ArrowBack, stringResource(R.string.common_back)) } },
                actions = {
                    FilterChip(
                        selected = follow,
                        onClick = {
                            follow = !follow
                            if (!follow) vm.refresh()
                        },
                        label = { Text(stringResource(R.string.logs_follow)) },
                        leadingIcon = if (follow) ({ Icon(Icons.Outlined.Check, contentDescription = null, Modifier.size(18.dp)) }) else null,
                    )
                    if (!follow) IconButton(onClick = vm::refresh) { Icon(Icons.Outlined.Refresh, stringResource(R.string.common_refresh)) }
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
            if (follow) {
                LogLines(followState.lines, truncated = false, filter = filter, live = true)
            } else {
                when (val s = state) {
                    UiState.Loading -> LoadingBox()
                    is UiState.Failed -> ErrorBox(s.message, vm::refresh)
                    is UiState.Loaded -> LogLines(s.data.lines, s.data.truncated, filter, live = false)
                }
            }
        }
    }
}

@Composable
private fun LogLines(all: List<String>, truncated: Boolean, filter: String, live: Boolean) {
    val lines = remember(all, filter) { all.matching(filter) }
    val listState = rememberLazyListState()
    val scope = rememberCoroutineScope()
    // Stick to the newest line until the user scrolls up; scrolling back down re-enables it.
    var stickToEnd by remember { mutableStateOf(true) }
    LaunchedEffect(listState) {
        snapshotFlow { listState.isScrollInProgress to listState.canScrollForward }
            .collect { (scrolling, canScrollForward) -> if (scrolling) stickToEnd = !canScrollForward }
    }
    // Open at the newest lines, like `tail`, and follow new ones.
    LaunchedEffect(lines) { if (stickToEnd && lines.isNotEmpty()) listState.scrollToItem(lines.lastIndex) }

    if (lines.isEmpty()) {
        Text(
            when {
                filter.isNotBlank() -> stringResource(R.string.logs_no_match, filter)
                live -> stringResource(R.string.logs_waiting)
                else -> stringResource(R.string.logs_empty)
            },
            modifier = Modifier.padding(16.dp),
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        return
    }

    Box(Modifier.fillMaxSize()) {
        SelectionContainer {
            LazyColumn(
                state = listState,
                contentPadding = PaddingValues(horizontal = 12.dp, vertical = 8.dp),
                modifier = Modifier.fillMaxSize(),
            ) {
                if (truncated && filter.isBlank()) {
                    item {
                        Text(
                            stringResource(R.string.logs_older_omitted),
                            style = MaterialTheme.typography.labelSmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                }
                items(lines) { line ->
                    Text(line, fontFamily = FontFamily.Monospace, fontSize = 11.sp, lineHeight = 14.sp)
                }
            }
        }
        if (live && !stickToEnd) {
            ExtendedFloatingActionButton(
                onClick = {
                    stickToEnd = true
                    scope.launch { listState.scrollToItem(lines.lastIndex) }
                },
                icon = { Icon(Icons.Outlined.ArrowDownward, contentDescription = null) },
                text = { Text(stringResource(R.string.logs_jump_latest)) },
                modifier = Modifier.align(Alignment.BottomCenter).padding(16.dp),
            )
        }
    }
}
