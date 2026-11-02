package name.levis.ichor.ui.workloads

import name.levis.ichor.data.MaskScoped
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.ArrowDownward
import androidx.compose.material3.ExtendedFloatingActionButton
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.ViewModel
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import name.levis.ichor.data.LogParser
import name.levis.ichor.ui.logs.AnsiLine
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.data.KubeBrowserRepository
import name.levis.ichor.data.StreamItem
import name.levis.ichor.model.FollowBuffer
import name.levis.ichor.model.POD_LOG_TAIL
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.logs.collectLogBatches
import name.levis.ichor.ui.userMessage

/** A followed pod log: its lines (the newest kept), whether it streams, the error it ended with. */
data class PodFollowState(val buffer: FollowBuffer = FollowBuffer(), val streaming: Boolean = false, val error: String? = null)

/** Follows a container's log through the Kubernetes API (`kubectl logs -f`) while [follow] runs. */
class PodLogFollowViewModel(private val browser: KubeBrowserRepository) : ViewModel() {
    private val _state = MutableStateFlow(PodFollowState())
    val state: StateFlow<PodFollowState> = _state.asStateFlow()

    private val mask = MaskScoped()

    /**
     * Starts over with the last lines of [container] ("" for the only one); runs until cancelled
     * or the log ends, and starts over after a screenshot mode switch.
     */
    suspend fun follow(namespace: String, pod: String, container: String) =
        mask.run(clear = ::clear) { followOnce(namespace, pod, container) }

    private suspend fun followOnce(namespace: String, pod: String, container: String) {
        _state.value = PodFollowState(streaming = true)
        try {
            browser.followPodLogs(namespace, pod, container, POD_LOG_TAIL).collectLogBatches(::apply)
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
        // Parsed off the main thread, as the Talos logs are: escapes out, colours kept as spans.
        val entries = withContext(Dispatchers.Default) { lines.map(LogParser::line) }
        val done = batch.filterIsInstance<StreamItem.Done>().lastOrNull()
        _state.update { it.copy(buffer = it.buffer.appendEntries(entries), streaming = it.streaming && done == null, error = done?.error ?: it.error) }
    }

    /** Forgets the lines once the sheet is gone. */
    fun clear() {
        _state.value = PodFollowState()
    }
}

/**
 * The followed lines, newest at the bottom: sticks to new lines until the user scrolls up,
 * then offers to jump back to the latest; scrolling back down sticks again.
 */
@Composable
internal fun FollowedLog(buffer: FollowBuffer) {
    val lines = buffer.lines
    if (lines.isEmpty()) {
        EmptyText(stringResource(R.string.logs_waiting))
        return
    }
    val listState = rememberLazyListState()
    val scope = rememberCoroutineScope()
    var stickToEnd by remember { mutableStateOf(true) }
    LaunchedEffect(listState) {
        snapshotFlow { listState.isScrollInProgress to listState.canScrollForward }
            .collect { (scrolling, canScrollForward) -> if (scrolling) stickToEnd = !canScrollForward }
    }
    LaunchedEffect(buffer.nextSeq) { if (stickToEnd) listState.scrollToItem(lines.lastIndex) }
    Box(Modifier.fillMaxSize()) {
        SelectionContainer {
            LazyColumn(Modifier.fillMaxSize(), state = listState, contentPadding = PaddingValues(horizontal = 16.dp)) {
                items(lines, key = { it.seq }) { line -> AnsiLine(line.text, line.spans, style = podLogTextStyle()) }
            }
        }
        if (!stickToEnd) {
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
