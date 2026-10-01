package name.levis.talosmobile.ui.live

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.repeatOnLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import name.levis.talosmobile.data.TalosRepository
import name.levis.talosmobile.model.NodeStats
import name.levis.talosmobile.model.StatsPoint
import name.levis.talosmobile.model.ratesBetween
import name.levis.talosmobile.ui.app
import name.levis.talosmobile.ui.factory
import name.levis.talosmobile.ui.theme.LocalChartColors
import name.levis.talosmobile.ui.theme.LocalStatusColors
import name.levis.talosmobile.ui.userMessage
import name.levis.talosmobile.util.formatBytes
import java.util.Locale

const val POLL_SECONDS = 2L
const val MAX_POINTS = 90 // 3 minutes of history

data class LiveState(val points: List<StatsPoint> = emptyList(), val error: String? = null, val cpuCount: Int = 0)

/** Polls NodeStats while the Live tab is visible; history survives tab switches. */
class LiveStatsViewModel(private val talos: TalosRepository, private val node: String) : ViewModel() {
    private val _state = MutableStateFlow(LiveState())
    val state: StateFlow<LiveState> = _state.asStateFlow()
    private var last: NodeStats? = null

    suspend fun poll() {
        while (true) {
            runCatching { talos.stats(node) }.fold(
                onSuccess = { sample ->
                    val point = last?.let { ratesBetween(it, sample) }
                    last = sample
                    _state.value = _state.value.let { s ->
                        s.copy(
                            points = if (point == null) s.points else (s.points + point).takeLast(MAX_POINTS),
                            error = null,
                            cpuCount = sample.cpuCount,
                        )
                    }
                },
                onFailure = { _state.value = _state.value.copy(error = it.userMessage()) },
            )
            delay(POLL_SECONDS * 1000)
        }
    }
}

@Composable
fun LiveStatsTab(
    node: String,
    vm: LiveStatsViewModel = viewModel(key = "live-$node", factory = factory { LiveStatsViewModel(app.talosRepository, node) }),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    val lifecycle = LocalLifecycleOwner.current.lifecycle
    // Poll only while visible: leaving the tab or backgrounding the app stops it.
    LaunchedEffect(lifecycle) { lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) { vm.poll() } }

    val colors = LocalChartColors.current
    val points = state.points
    val times = points.map { it.at }
    fun rate(v: Float) = formatBytes(v.toLong()) + "/s"

    LazyColumn(
        contentPadding = PaddingValues(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
        modifier = Modifier.fillMaxSize(),
    ) {
        state.error?.let { item { Text(it, color = LocalStatusColors.current.bad, style = MaterialTheme.typography.bodySmall) } }
        if (points.isEmpty()) {
            item { Text("Collecting samples every ${POLL_SECONDS}s…", color = MaterialTheme.colorScheme.onSurfaceVariant) }
        }
        item {
            LiveChart(
                title = "CPU" + if (state.cpuCount > 0) " (${state.cpuCount} threads)" else "",
                series = listOf(Series("CPU", colors.first, points.map { it.cpuPercent })),
                times = times,
                format = { String.format(Locale.ROOT, "%.0f%%", it) },
                gridColor = colors.grid,
                fixedMax = 100f,
            )
        }
        item {
            LiveChart(
                title = "Memory" + (points.lastOrNull()?.let { " (${formatBytes(it.memUsed)} used)" } ?: ""),
                series = listOf(Series("Memory", colors.first, points.map { it.memPercent })),
                times = times,
                format = { String.format(Locale.ROOT, "%.0f%%", it) },
                gridColor = colors.grid,
                fixedMax = 100f,
            )
        }
        item {
            LiveChart(
                title = "Network",
                series = listOf(
                    Series("in", colors.first, points.map { it.rxPerSec }),
                    Series("out", colors.second, points.map { it.txPerSec }),
                ),
                times = times,
                format = ::rate,
                gridColor = colors.grid,
            )
        }
        item {
            LiveChart(
                title = "Disk",
                series = listOf(
                    Series("read", colors.first, points.map { it.readPerSec }),
                    Series("write", colors.second, points.map { it.writePerSec }),
                ),
                times = times,
                format = ::rate,
                gridColor = colors.grid,
            )
        }
        item {
            LiveChart(
                title = "Load (1 min)",
                series = listOf(Series("Load", colors.first, points.map { it.load1 })),
                times = times,
                format = { String.format(Locale.ROOT, "%.2f", it) },
                gridColor = colors.grid,
            )
        }
    }
}
