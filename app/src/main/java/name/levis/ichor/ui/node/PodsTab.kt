package name.levis.ichor.ui.node

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
import androidx.compose.material.icons.outlined.Warning
import androidx.compose.material3.FilterChip
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
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
import name.levis.ichor.R
import name.levis.ichor.data.TalosRepository
import androidx.compose.foundation.clickable
import name.levis.ichor.model.ContainerInfo
import name.levis.ichor.model.ContainerRow
import name.levis.ichor.model.ContainerSample
import name.levis.ichor.model.ContainerSort
import name.levis.ichor.model.PodGroup
import name.levis.ichor.model.containerRows
import name.levis.ichor.model.podGroups
import name.levis.ichor.model.running
import name.levis.ichor.model.statusLabel
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SearchField
import name.levis.ichor.ui.components.emptyOrNoMatch
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.userMessage
import name.levis.ichor.util.formatBytes
import name.levis.ichor.util.formatPercent

private const val PODS_POLL_SECONDS = 3L

data class PodsState(val rows: List<ContainerRow>? = null, val error: String? = null)

/** Polls the node's containers while the tab is visible; keeps the previous sample for CPU%. */
class PodsViewModel(private val talos: TalosRepository, private val node: String) : ViewModel() {
    private val _state = MutableStateFlow(PodsState())
    val state: StateFlow<PodsState> = _state.asStateFlow()
    private var last: ContainerSample? = null

    suspend fun poll() {
        while (true) {
            runCatching { talos.containers(node) }.fold(
                onSuccess = { sample ->
                    val rows = containerRows(last, sample)
                    last = sample
                    _state.value = PodsState(rows = rows)
                },
                onFailure = {
                    // Leaving the tab cancels the call: that is not an error to show on return.
                    if (it is CancellationException) throw it
                    _state.value = _state.value.copy(error = it.userMessage())
                },
            )
            delay(PODS_POLL_SECONDS * 1000)
        }
    }
}

@Composable
fun PodsTab(
    node: String,
    onContainer: (ContainerInfo) -> Unit,
    vm: PodsViewModel = viewModel(key = "pods-$node", factory = factory { PodsViewModel(app.talosRepository, node) }),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    val lifecycle = LocalLifecycleOwner.current.lifecycle
    // Poll only while visible: leaving the tab or backgrounding the app stops it.
    LaunchedEffect(lifecycle) { lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) { vm.poll() } }

    var filter by rememberSaveable { mutableStateOf("") }
    var sort by rememberSaveable { mutableStateOf(ContainerSort.CPU) }

    val rows = state.rows
    if (rows == null) {
        state.error?.let { PodsError(it, Modifier.padding(16.dp)) } ?: LoadingBox()
        return
    }
    val groups = remember(rows, filter, sort) { rows.podGroups(filter, sort) }

    Column(Modifier.fillMaxSize()) {
        Column(Modifier.padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            state.error?.let { PodsError(it) }
            MutedText(pluralStringResource(R.plurals.node_pods_summary, rows.size, rows.size, formatBytes(rows.sumOf { it.info.memory })))
            SearchField(filter, { filter = it }, stringResource(R.string.node_pods_filter), Modifier.fillMaxWidth())
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
                Text(stringResource(R.string.node_processes_sort), style = MaterialTheme.typography.labelMedium)
                FilterChip(
                    selected = sort == ContainerSort.CPU,
                    onClick = { sort = ContainerSort.CPU },
                    label = { Text(stringResource(R.string.node_processes_sort_cpu)) },
                )
                FilterChip(
                    selected = sort == ContainerSort.MEMORY,
                    onClick = { sort = ContainerSort.MEMORY },
                    label = { Text(stringResource(R.string.node_processes_sort_memory)) },
                )
            }
        }
        HorizontalDivider()
        if (groups.isEmpty()) {
            EmptyText(emptyOrNoMatch(filter, R.string.node_pods_empty, R.string.node_pods_no_match))
            return@Column
        }
        LazyColumn(Modifier.fillMaxSize()) {
            groups.forEach { group ->
                item(key = "pod|${group.namespace}|${group.pod}") { PodHeader(group) }
                items(group.containers, key = { "c|${it.info.id}" }) { row ->
                    ContainerItem(row, onClick = { onContainer(row.info) })
                    HorizontalDivider()
                }
            }
        }
    }
}

@Composable
private fun PodsError(message: String, modifier: Modifier = Modifier) {
    Text(message, color = LocalStatusColors.current.bad, style = MaterialTheme.typography.bodySmall, modifier = modifier)
}

private fun cpu(percent: Double) = formatPercent(percent)

@Composable
private fun PodHeader(group: PodGroup) {
    Surface(color = MaterialTheme.colorScheme.surfaceContainer, modifier = Modifier.fillMaxWidth()) {
        Row(Modifier.padding(horizontal = 16.dp, vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
            Column(Modifier.weight(1f)) {
                Text(
                    group.pod.ifEmpty { "—" },
                    style = MaterialTheme.typography.titleSmall,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                if (group.namespace.isNotEmpty()) {
                    Text(group.namespace, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
            }
            Text(cpu(group.cpuPercent), style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace)
            Text(
                formatBytes(group.memory),
                style = MaterialTheme.typography.bodyMedium,
                fontFamily = FontFamily.Monospace,
                modifier = Modifier.padding(start = 12.dp),
            )
        }
    }
}

@Composable
private fun ContainerItem(row: ContainerRow, onClick: () -> Unit) {
    val c = row.info
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    val warn = LocalStatusColors.current.warn
    // Tapping a container opens its log.
    Column(Modifier.fillMaxWidth().clickable(onClick = onClick).padding(start = 28.dp, end = 16.dp, top = 6.dp, bottom = 6.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                c.name.ifEmpty { c.id.take(12) },
                style = MaterialTheme.typography.bodyMedium,
                fontWeight = FontWeight.Bold,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
            Text(cpu(row.cpuPercent), style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace)
            Text(
                formatBytes(c.memory),
                style = MaterialTheme.typography.bodyMedium,
                fontFamily = FontFamily.Monospace,
                modifier = Modifier.padding(start = 12.dp),
            )
        }
        Text(
            c.image,
            style = MaterialTheme.typography.bodySmall,
            fontFamily = FontFamily.Monospace,
            color = muted,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
        Row(verticalAlignment = Alignment.CenterVertically) {
            if (!c.running) {
                Icon(Icons.Outlined.Warning, contentDescription = null, tint = warn, modifier = Modifier.size(14.dp).padding(end = 4.dp))
            }
            Text(
                stringResource(R.string.node_pods_details, c.statusLabel, c.pid),
                style = MaterialTheme.typography.labelSmall,
                color = if (c.running) muted else warn,
            )
        }
    }
}
