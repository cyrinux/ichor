package name.levis.ichor.ui.logs

import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.ArrowDownward
import androidx.compose.material3.ExtendedFloatingActionButton
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
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
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.model.LogLevelCounts
import name.levis.ichor.model.LogLevelFilter
import name.levis.ichor.model.LogRow
import name.levis.ichor.model.SeqLogEntry
import name.levis.ichor.model.levelCounts
import name.levis.ichor.model.logRows
import name.levis.ichor.model.matchingText
import java.time.ZoneId
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.MonoScreen
import name.levis.ichor.ui.components.ScalableMonoText
import name.levis.ichor.ui.components.monoTextStyle
import name.levis.ichor.ui.components.monoTextActions

/** How the log is shown: text [filter], [level] filter, or the [raw] lines as received. */
data class LogView(val filter: String, val level: LogLevelFilter, val raw: Boolean, val onLevel: (LogLevelFilter) -> Unit)

@Composable
fun LogContent(entries: List<SeqLogEntry>, truncated: Boolean, view: LogView, live: Boolean) {
    val matched = remember(entries, view.filter) { entries.matchingText(view.filter) }
    if (view.raw) {
        val lines = remember(matched) { matched.map { it.entry.text } }
        LogBody(lines.size, truncated && view.filter.isBlank(), lines, view, live) {
            items(lines) { line -> Text(line, style = monoTextStyle(), modifier = Modifier.monoTextActions()) }
        }
        return
    }
    val counts = remember(matched) { levelCounts(matched) }
    val zone = remember { ZoneId.systemDefault() }
    val rows = remember(matched, view.level) { logRows(matched, view.level, zone) }
    val palette = rememberLogPalette()
    var expanded by remember { mutableStateOf(emptySet<String>()) }
    Column {
        LevelChips(view.level, counts, view.onLevel)
        val header = truncated && view.filter.isBlank() && view.level == LogLevelFilter.ALL
        LogBody(rows.size, header, rows, view, live) {
            items(rows, key = { it.key }, contentType = { it::class }) { row ->
                when (row) {
                    is LogRow.Day -> LogDayRow(row)
                    is LogRow.Entry -> LogEntryRow(row, palette, zone, row.key in expanded) {
                        expanded = if (row.key in expanded) expanded - row.key else expanded + row.key
                    }
                }
            }
        }
    }
}

@Composable
private fun LevelChips(selected: LogLevelFilter, counts: LogLevelCounts, onSelect: (LogLevelFilter) -> Unit) {
    Row(
        Modifier.horizontalScroll(rememberScrollState()).padding(horizontal = 12.dp),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        LogLevelFilter.entries.forEach { filter ->
            val label = when (filter) {
                LogLevelFilter.ALL -> R.string.logs_level_all
                LogLevelFilter.WARNINGS -> R.string.logs_level_warnings
                LogLevelFilter.ERRORS -> R.string.logs_level_errors
            }
            FilterChip(
                selected = filter == selected,
                onClick = { onSelect(filter) },
                label = { Text(stringResource(label, counts.of(filter))) },
            )
        }
    }
}

/**
 * A list of [count] items opened at the end, like `tail`, that sticks to new items until the
 * user scrolls up. [content] adds the items after the optional "older omitted" [header].
 */
@Composable
private fun LogBody(count: Int, header: Boolean, version: Any, view: LogView, live: Boolean, content: LazyListScope.() -> Unit) {
    if (count == 0) {
        EmptyText(
            when {
                view.filter.isNotBlank() -> stringResource(R.string.logs_no_match, view.filter)
                !view.raw && view.level != LogLevelFilter.ALL -> stringResource(R.string.logs_no_level_match)
                live -> stringResource(R.string.logs_waiting)
                else -> stringResource(R.string.logs_empty)
            },
        )
        return
    }
    val lastIndex = count - 1 + if (header) 1 else 0
    val listState = rememberLazyListState()
    val scope = rememberCoroutineScope()
    // Stick to the newest line until the user scrolls up; scrolling back down re-enables it.
    var stickToEnd by remember { mutableStateOf(true) }
    LaunchedEffect(listState) {
        snapshotFlow { listState.isScrollInProgress to listState.canScrollForward }
            .collect { (scrolling, canScrollForward) -> if (scrolling) stickToEnd = !canScrollForward }
    }
    // Open at the newest lines, like `tail`, and follow new ones.
    LaunchedEffect(version) { if (stickToEnd) listState.scrollToItem(lastIndex) }

    ScalableMonoText(MonoScreen.LOGS, Modifier.fillMaxSize()) {
        val list = @Composable {
            LazyColumn(
                state = listState,
                contentPadding = PaddingValues(horizontal = 12.dp, vertical = 8.dp),
                modifier = Modifier.fillMaxSize(),
            ) {
                if (header) {
                    item(key = "older") {
                        Text(
                            stringResource(R.string.logs_older_omitted),
                            style = MaterialTheme.typography.labelSmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                }
                content()
            }
        }
        // Raw lines are selectable as a whole; structured rows expand to a selectable line.
        if (view.raw) SelectionContainer { list() } else list()
        if (live && !stickToEnd) {
            ExtendedFloatingActionButton(
                onClick = {
                    stickToEnd = true
                    scope.launch { listState.scrollToItem(lastIndex) }
                },
                icon = { Icon(Icons.Outlined.ArrowDownward, contentDescription = null) },
                text = { Text(stringResource(R.string.logs_jump_latest)) },
                modifier = Modifier.align(Alignment.BottomCenter).padding(16.dp),
            )
        }
    }
}
