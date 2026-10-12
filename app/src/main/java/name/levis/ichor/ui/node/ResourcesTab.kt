package name.levis.ichor.ui.node

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material3.Card
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.R
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.data.resourcesKey
import name.levis.ichor.model.CgroupReport
import name.levis.ichor.model.NodeResources
import name.levis.ichor.model.NodeTime
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.DataFreshness
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.InfoRow
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.UsageBar
import name.levis.ichor.ui.components.localizedDuration
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.history.NodeMemoryHistory
import name.levis.ichor.util.formatBytes
import name.levis.ichor.util.usedFraction
import java.util.Locale

class ResourcesViewModel(private val talos: TalosRepository, private val node: String) : LoadingViewModel<NodeResources>() {
    override val keepsDataOnFailure = true
    override fun cached(): TalosRepository.Timed<NodeResources>? = talos.cached(resourcesKey(node))
    override val restores get() = talos.restores
    override suspend fun fetch() = talos.resources(node)
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun ResourcesTab(
    node: String,
    /** Opens the Cgroups tab; null hides the pressure card (the role cannot read cgroups). */
    onPressureDetails: (() -> Unit)?,
    pressure: PressureViewModel = viewModel(key = "pressure-$node", factory = factory { PressureViewModel(app.talosRepository, node) }),
    vm: ResourcesViewModel = viewModel(key = "resources-$node", factory = factory { ResourcesViewModel(app.talosRepository, node) }),
    clock: NodeTimeViewModel = viewModel(key = "time-$node", factory = factory { NodeTimeViewModel(app.talosRepository, node) }),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    val clockState by clock.state.collectAsStateWithLifecycle()
    val pressureState by pressure.state.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) {
        if (state == UiState.Loading) vm.refresh()
        if (clockState == UiState.Loading) clock.refresh()
    }
    // Keyed on access: the config (and so the role) can arrive after the first frame.
    val showsPressure = onPressureDetails != null
    LaunchedEffect(showsPressure) {
        if (showsPressure && pressureState == UiState.Loading) pressure.refresh()
    }

    when (val s = state) {
        UiState.Loading -> LoadingBox()
        is UiState.Failed -> ErrorBox(s.message, vm::refresh)
        is UiState.Loaded -> Column(Modifier.fillMaxSize()) {
            PullToRefreshBox(
                isRefreshing = s.refreshing,
                onRefresh = {
                    vm.refresh()
                    clock.refresh()
                    if (onPressureDetails != null) pressure.refresh()
                },
                modifier = Modifier.weight(1f),
            ) {
                ResourcesContent(node, s.data, clockState, pressure = onPressureDetails?.let { pressureState to it })
            }
            DataFreshness(s, edgeToEdge = false)
        }
    }
}

@Composable
private fun ResourcesContent(node: String, r: NodeResources, clock: UiState<NodeTime>, pressure: Pair<UiState<CgroupReport>, () -> Unit>?) {
    val uptime = if (r.bootTime > 0) localizedDuration(System.currentTimeMillis() / 1000 - r.bootTime) else "—"
    val cpu = if (r.cpuModel.isBlank()) {
        pluralStringResource(R.plurals.node_cpu_threads, r.cpuCount, r.cpuCount)
    } else {
        "${r.cpuCount} × ${r.cpuModel}"
    }
    LazyColumn(contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        item {
            Card(Modifier.fillMaxWidth()) {
                Column(Modifier.padding(16.dp)) {
                    SectionTitle(stringResource(R.string.node_section_system))
                    InfoRow(stringResource(R.string.node_uptime), uptime)
                    InfoRow(stringResource(R.string.node_cpu), cpu)
                    InfoRow(stringResource(R.string.node_load), String.format(Locale.ROOT, "%.2f  %.2f  %.2f", r.load1, r.load5, r.load15))
                    if (r.cpuCount > 0) UsageBar((r.load1 / r.cpuCount).toFloat().coerceIn(0f, 1f), Modifier.padding(top = 4.dp))
                    ClockOffsetRow(clock)
                }
            }
        }
        item {
            Card(Modifier.fillMaxWidth()) {
                Column(Modifier.padding(16.dp)) {
                    SectionTitle(stringResource(R.string.node_section_memory))
                    val used = r.memTotal - r.memAvailable
                    InfoRow(stringResource(R.string.node_memory_used), "${formatBytes(used)} / ${formatBytes(r.memTotal)}")
                    UsageBar(usedFraction(r.memTotal, r.memAvailable), Modifier.padding(top = 4.dp))
                    NodeMemoryHistory(node, Modifier.padding(top = 8.dp))
                }
            }
        }
        // Right after memory: pressure says whether CPU, memory or disk actually hold tasks back.
        pressure?.let { (state, onDetails) -> item { PressureCard(state, onDetails) } }
        if (r.mounts.isNotEmpty()) {
            item {
                Card(Modifier.fillMaxWidth()) {
                    Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                        SectionTitle(stringResource(R.string.node_section_disks))
                        r.mounts.forEach { m ->
                            Column {
                                Row {
                                    Text(m.mountedOn, style = MaterialTheme.typography.bodyMedium, modifier = Modifier.weight(1f))
                                    Text(
                                        "${formatBytes(m.size - m.available)} / ${formatBytes(m.size)}",
                                        style = MaterialTheme.typography.bodySmall,
                                    )
                                }
                                Text(
                                    m.filesystem,
                                    style = MaterialTheme.typography.bodySmall,
                                    fontFamily = FontFamily.Monospace,
                                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                                )
                                UsageBar(usedFraction(m.size, m.available), Modifier.padding(top = 4.dp))
                            }
                        }
                    }
                }
            }
        }
    }
}
