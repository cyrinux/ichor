package name.levis.ichor.ui.node

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.KeyboardArrowRight
import androidx.compose.material.icons.outlined.KeyboardArrowDown
import androidx.compose.material3.FilterChip
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.dp
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import name.levis.ichor.R
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.model.CgroupReport
import name.levis.ichor.model.CgroupRow
import name.levis.ichor.model.CgroupSort
import name.levis.ichor.model.PressureLevel
import name.levis.ichor.model.cgroupRows
import name.levis.ichor.model.defaultExpandedCgroups
import name.levis.ichor.model.pressureLevel
import name.levis.ichor.ui.PollWhileStarted
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.live.LiveChart
import name.levis.ichor.ui.live.Series
import name.levis.ichor.ui.pollEvery
import name.levis.ichor.ui.theme.LocalChartColors
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.userMessage
import name.levis.ichor.util.formatBytes
import name.levis.ichor.util.formatPercent
import name.levis.ichor.ui.components.expandable

/** A copy of /sys/fs/cgroup (several MB on a busy node) is heavy on mobile data: poll slowly. */
private const val CGROUPS_POLL_SECONDS = 10L

/** Pressure samples the chart keeps: 5 minutes at one per poll. */
private const val CGROUPS_HISTORY_POINTS = 30

data class CgroupsState(
    val previous: CgroupReport? = null,
    val current: CgroupReport? = null,
    val error: String? = null,
    /** The node's pressure at each poll, oldest first, without the tree, for the chart. */
    val history: List<CgroupReport> = emptyList(),
)

/** Polls the node's cgroups while the tab is visible; keeps the previous sample for rates. */
class CgroupsViewModel(private val talos: TalosRepository, private val node: String) : ViewModel() {
    private val _state = MutableStateFlow(CgroupsState())
    val state: StateFlow<CgroupsState> = _state.asStateFlow()

    suspend fun poll(): Nothing = pollEvery({ CGROUPS_POLL_SECONDS * 1000 }, { talos.cgroups(node) }, { _state.value = _state.value.copy(error = it.userMessage()) }) { report ->
        val last = _state.value
        _state.value = CgroupsState(
            previous = last.current,
            current = report,
            history = (last.history + report.copy(root = null)).takeLast(CGROUPS_HISTORY_POINTS),
        )
    }
}

@Composable
fun CgroupsTab(
    node: String,
    vm: CgroupsViewModel = viewModel(key = "cgroups-$node", factory = factory { CgroupsViewModel(app.talosRepository, node) }),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    PollWhileStarted { vm.poll() }

    var sort by rememberSaveable { mutableStateOf(CgroupSort.MEMORY) }
    var expanded by remember { mutableStateOf<Set<String>?>(null) }

    val current = state.current
    if (current == null) {
        state.error?.let { CgroupsError(it, Modifier.padding(16.dp)) } ?: LoadingBox()
        return
    }
    val open = expanded ?: defaultExpandedCgroups(current)
    val rows = remember(state, open, sort) { cgroupRows(state.previous, current, open, sort) }

    Column(Modifier.fillMaxSize()) {
        Column(Modifier.padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            state.error?.let { CgroupsError(it) }
            MutedText(stringResource(R.string.node_cgroups_caption))
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
                Text(stringResource(R.string.node_processes_sort), style = MaterialTheme.typography.labelMedium)
                listOf(
                    CgroupSort.MEMORY to R.string.node_processes_sort_memory,
                    CgroupSort.CPU to R.string.node_processes_sort_cpu,
                    CgroupSort.PRESSURE to R.string.node_section_pressure,
                ).forEach { (option, label) ->
                    FilterChip(selected = sort == option, onClick = { sort = option }, label = { Text(stringResource(label)) })
                }
            }
        }
        HorizontalDivider()
        LazyColumn(Modifier.fillMaxSize()) {
            item(key = "pressure-chart") { PressureChart(state.history) }
            items(rows, key = { it.path }) { row ->
                CgroupItem(row, expanded = row.path in open) {
                    expanded = if (row.path in open) open - row.path else open + row.path
                }
                HorizontalDivider()
            }
        }
    }
}

@Composable
private fun CgroupsError(message: String, modifier: Modifier = Modifier) {
    Text(message, color = LocalStatusColors.current.bad, style = MaterialTheme.typography.bodySmall, modifier = modifier)
}

@Composable
private fun CgroupItem(row: CgroupRow, expanded: Boolean, onToggle: () -> Unit) {
    val n = row.node
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    Row(
        Modifier
            .fillMaxWidth()
            .expandable(expanded, enabled = row.hasChildren, onToggle = onToggle)
            .padding(start = 8.dp + 16.dp * row.depth, end = 16.dp, top = 8.dp, bottom = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        if (row.hasChildren) {
            Icon(
                if (expanded) Icons.Outlined.KeyboardArrowDown else Icons.AutoMirrored.Outlined.KeyboardArrowRight,
                contentDescription = null,
                tint = muted,
            )
        } else {
            Spacer(Modifier.width(24.dp))
        }
        Column(Modifier.weight(1f).padding(start = 4.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    n.name,
                    style = MaterialTheme.typography.bodyMedium,
                    fontWeight = if (n.kind == "service" || n.kind == "pod") FontWeight.Bold else FontWeight.Normal,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.weight(1f),
                )
                Text(
                    row.cpuPercent?.let { formatPercent(it) } ?: "—",
                    style = MaterialTheme.typography.bodySmall,
                    fontFamily = FontFamily.Monospace,
                    modifier = Modifier.padding(start = 8.dp),
                )
                Text(
                    formatBytes(n.memCurrent),
                    style = MaterialTheme.typography.bodySmall,
                    fontFamily = FontFamily.Monospace,
                    modifier = Modifier.padding(start = 12.dp),
                )
            }
            CgroupNotes(row)
        }
    }
}

/** Only what stands out: a memory limit, OOM kills, disk traffic and pressure worth a look. */
@Composable
private fun CgroupNotes(row: CgroupRow) {
    val n = row.node
    val notes = buildList {
        if (n.memMax > 0) add(stringResource(R.string.node_cgroups_limit, formatBytes(n.memMax)))
        row.ioPerSecond?.takeIf { it >= 1024 }?.let { add(stringResource(R.string.node_cgroups_io, formatBytes(it.toLong()))) }
        if (n.oomKills > 0) add(pluralStringResource(R.plurals.node_cgroups_oom_short, n.oomKills.toInt(), n.oomKills.toInt()))
    }
    val pressure = n.pressure
    val waiting = pressure?.let {
        listOf(
            stringResource(R.string.node_pressure_cpu) to it.cpu.some10,
            stringResource(R.string.node_pressure_memory) to it.memory.some10,
            stringResource(R.string.node_pressure_io) to it.io.some10,
        ).filter { (_, v) -> pressureLevel(v) != PressureLevel.OK }
    }.orEmpty().map { (label, v) ->
        stringResource(R.string.node_cgroups_waiting, label, formatPercent(v)) to levelColor(pressureLevel(v))
    }
    if (notes.isEmpty() && waiting.isEmpty()) return
    // One text so the line wraps as a whole instead of squeezing the pressure into a narrow column.
    val text = buildAnnotatedString {
        append(notes.joinToString(" · "))
        waiting.forEach { (note, color) ->
            if (length > 0) append(" · ")
            withStyle(SpanStyle(color = color)) { append(note) }
        }
    }
    Text(text, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
}

/**
 * The node's PSI "some" 10 s averages at each poll while the tab is open: how the waiting for
 * CPU, memory and disk moves. The scale reaches at least 10 % so a quiet node reads as flat.
 */
@Composable
private fun PressureChart(history: List<CgroupReport>) {
    val colors = LocalChartColors.current
    val series = listOf(
        Series(stringResource(R.string.node_pressure_cpu), colors.first, history.map { it.pressure.cpu.some10.toFloat() }),
        Series(stringResource(R.string.node_pressure_memory), colors.second, history.map { it.pressure.memory.some10.toFloat() }),
        Series(stringResource(R.string.node_pressure_io), colors.third, history.map { it.pressure.io.some10.toFloat() }),
    )
    val peak = series.maxOf { it.values.maxOrNull() ?: 0f }
    LiveChart(
        title = stringResource(R.string.node_section_pressure),
        series = series,
        times = history.map { it.at },
        format = { formatPercent(it.toDouble()) },
        gridColor = colors.grid,
        modifier = Modifier.padding(16.dp),
        fixedMax = (peak * 1.15f).coerceIn(PRESSURE_CHART_FLOOR, 100f),
        maxPoints = CGROUPS_HISTORY_POINTS,
        pollSeconds = CGROUPS_POLL_SECONDS,
    )
}

private const val PRESSURE_CHART_FLOOR = 10f
