package name.levis.talosmobile.ui.node

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material3.FilterChip
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.repeatOnLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import name.levis.talosmobile.R
import name.levis.talosmobile.data.TalosRepository
import name.levis.talosmobile.model.ProcessRow
import name.levis.talosmobile.model.ProcessSample
import name.levis.talosmobile.model.ProcessSort
import name.levis.talosmobile.model.filterAndSort
import name.levis.talosmobile.model.processRows
import name.levis.talosmobile.ui.app
import name.levis.talosmobile.ui.components.LoadingBox
import name.levis.talosmobile.ui.factory
import name.levis.talosmobile.ui.live.POLL_SECONDS
import name.levis.talosmobile.ui.theme.LocalStatusColors
import name.levis.talosmobile.ui.userMessage
import name.levis.talosmobile.util.formatBytes
import java.util.Locale

data class ProcessesState(val rows: List<ProcessRow>? = null, val error: String? = null)

/** Polls the node's processes while the tab is visible; keeps the previous sample for CPU%. */
class ProcessesViewModel(private val talos: TalosRepository, private val node: String) : ViewModel() {
    private val _state = MutableStateFlow(ProcessesState())
    val state: StateFlow<ProcessesState> = _state.asStateFlow()
    private var last: ProcessSample? = null

    suspend fun poll() {
        while (true) {
            runCatching { talos.processes(node) }.fold(
                onSuccess = { sample ->
                    val rows = processRows(last, sample)
                    last = sample
                    _state.value = ProcessesState(rows = rows)
                },
                onFailure = {
                    // Leaving the tab cancels the call: that is not an error to show on return.
                    if (it is CancellationException) throw it
                    _state.value = _state.value.copy(error = it.userMessage())
                },
            )
            delay(POLL_SECONDS * 1000)
        }
    }
}

@Composable
fun ProcessesTab(
    node: String,
    vm: ProcessesViewModel = viewModel(key = "processes-$node", factory = factory { ProcessesViewModel(app.talosRepository, node) }),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    val lifecycle = LocalLifecycleOwner.current.lifecycle
    // Poll only while visible: leaving the tab or backgrounding the app stops it.
    LaunchedEffect(lifecycle) { lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) { vm.poll() } }

    var filter by rememberSaveable { mutableStateOf("") }
    var sort by rememberSaveable { mutableStateOf(ProcessSort.CPU) }
    var expanded by remember { mutableStateOf(emptySet<Int>()) }

    val rows = state.rows
    if (rows == null) {
        state.error?.let { ProcessesError(it, Modifier.padding(16.dp)) } ?: LoadingBox()
        return
    }
    val shown = remember(rows, filter, sort) { rows.filterAndSort(filter, sort) }

    Column(Modifier.fillMaxSize()) {
        Column(Modifier.padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            state.error?.let { ProcessesError(it) }
            Text(
                pluralStringResource(R.plurals.node_processes_summary, rows.size, rows.size, formatBytes(rows.sumOf { it.info.rss })),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            OutlinedTextField(
                value = filter,
                onValueChange = { filter = it },
                placeholder = { Text(stringResource(R.string.node_processes_filter)) },
                leadingIcon = { Icon(Icons.Outlined.Search, contentDescription = null) },
                singleLine = true,
                modifier = Modifier.fillMaxWidth(),
            )
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
                Text(stringResource(R.string.node_processes_sort), style = MaterialTheme.typography.labelMedium)
                FilterChip(
                    selected = sort == ProcessSort.CPU,
                    onClick = { sort = ProcessSort.CPU },
                    label = { Text(stringResource(R.string.node_processes_sort_cpu)) },
                )
                FilterChip(
                    selected = sort == ProcessSort.MEMORY,
                    onClick = { sort = ProcessSort.MEMORY },
                    label = { Text(stringResource(R.string.node_processes_sort_memory)) },
                )
            }
        }
        HorizontalDivider()
        LazyColumn(Modifier.fillMaxSize()) {
            items(shown, key = { it.info.pid }) { row ->
                val pid = row.info.pid
                ProcessItem(
                    row = row,
                    expanded = pid in expanded,
                    onClick = { expanded = if (pid in expanded) expanded - pid else expanded + pid },
                )
                HorizontalDivider()
            }
        }
    }
}

@Composable
private fun ProcessesError(message: String, modifier: Modifier = Modifier) {
    Text(message, color = LocalStatusColors.current.bad, style = MaterialTheme.typography.bodySmall, modifier = modifier)
}

@Composable
private fun ProcessItem(row: ProcessRow, expanded: Boolean, onClick: () -> Unit) {
    val p = row.info
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    Column(
        Modifier
            .fillMaxWidth()
            .clickable(onClick = onClick)
            .padding(horizontal = 16.dp, vertical = 8.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                p.command.ifEmpty { "[${p.pid}]" },
                style = MaterialTheme.typography.bodyMedium,
                fontWeight = FontWeight.Bold,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
            Text(
                String.format(Locale.ROOT, "%.1f%%", row.cpuPercent),
                style = MaterialTheme.typography.bodyMedium,
                fontFamily = FontFamily.Monospace,
                modifier = Modifier.padding(start = 8.dp),
            )
            Text(
                formatBytes(p.rss),
                style = MaterialTheme.typography.bodyMedium,
                fontFamily = FontFamily.Monospace,
                modifier = Modifier.padding(start = 12.dp),
            )
        }
        if (p.args.isNotBlank()) {
            Text(
                p.args,
                style = MaterialTheme.typography.bodySmall,
                fontFamily = FontFamily.Monospace,
                color = muted,
                maxLines = if (expanded) Int.MAX_VALUE else 1,
                overflow = TextOverflow.Ellipsis,
            )
        }
        Text(
            stringResource(R.string.node_processes_details, p.pid, p.state, p.threads),
            style = MaterialTheme.typography.labelSmall,
            color = muted,
        )
    }
}
