package name.levis.ichor.ui.metrics

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material.icons.outlined.Tune
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ExtendedFloatingActionButton
import androidx.compose.material3.Icon
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.repeatOnLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import kotlinx.coroutines.delay
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.activeSummary
import name.levis.ichor.model.PromPanel
import name.levis.ichor.model.legend
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.factory

private const val AUTO_REFRESH_MS = 60_000L

/**
 * PromQL panels of the cluster on screen, from its Prometheus, Mimir, Thanos or
 * VictoriaMetrics: found in the cluster (service proxy) or at a URL set by the user.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun MetricsScreen(onBack: () -> Unit) {
    val app = LocalContext.current.applicationContext as TalosApp
    val config by app.configRepository.config.collectAsStateWithLifecycle()
    val invalidations by app.talosRepository.invalidations.collectAsStateWithLifecycle()
    val fingerprint = config?.activeSummary?.fingerprint ?: return
    val key = "metrics-$fingerprint-$invalidations"
    val noSource = stringResource(R.string.metrics_none_found)
    val vm: MetricsViewModel = viewModel(key = key, factory = factory { MetricsViewModel(app.talosRepository, app.metricsStore, fingerprint, noSource) })
    val state by vm.state.collectAsStateWithLifecycle()
    var editing by remember { mutableStateOf<PromPanel?>(null) }
    var sourceOpen by remember { mutableStateOf(false) }
    val lifecycle = LocalLifecycleOwner.current.lifecycle

    LaunchedEffect(key) { vm.load() }
    // Quiet refresh while on screen.
    LaunchedEffect(vm, lifecycle) {
        lifecycle.repeatOnLifecycle(Lifecycle.State.RESUMED) {
            while (true) {
                delay(AUTO_REFRESH_MS)
                vm.refresh(quiet = true)
            }
        }
    }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text(stringResource(R.string.metrics_title)) },
                navigationIcon = { BackButton(onBack) },
                actions = {
                    TooltipIconButton(Icons.Outlined.Refresh, stringResource(R.string.common_refresh), onClick = { vm.refresh() })
                    TooltipIconButton(Icons.Outlined.Tune, stringResource(R.string.metrics_source), onClick = { sourceOpen = true })
                },
            )
        },
        floatingActionButton = {
            if (state.config.source != null) {
                ExtendedFloatingActionButton(
                    onClick = { editing = PromPanel(id = "", title = "", query = "") },
                    icon = { Icon(Icons.Outlined.Add, null) },
                    text = { Text(stringResource(R.string.metrics_add_panel)) },
                )
            }
        },
    ) { padding ->
        when {
            !state.loaded -> LoadingBox(Modifier.padding(padding).fillMaxSize())
            state.config.source == null -> NoSource(state, Modifier.padding(padding), onSetUp = { sourceOpen = true }, onSearch = { vm.discover(autoSelect = true) })
            else -> LazyColumn(
                Modifier.padding(padding),
                contentPadding = PaddingValues(start = 16.dp, end = 16.dp, top = 8.dp, bottom = 96.dp),
                verticalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                item { MutedText(stringResource(R.string.metrics_source_line, state.config.source!!.label)) }
                item { RangeRow(state.range, vm::setRange) }
                state.error?.let { item { Text(it, color = MaterialTheme.colorScheme.error) } }
                if (state.config.panels.isEmpty()) item { MutedText(stringResource(R.string.metrics_no_panels)) }
                val panels = state.config.panels
                items(panels, key = { it.id }) { panel ->
                    PanelCard(
                        panel = panel,
                        result = state.results[panel.id] ?: PanelResult(loading = true),
                        first = panel == panels.first(),
                        last = panel == panels.last(),
                        onEdit = { editing = panel },
                        onMove = { vm.movePanel(panel.id, it) },
                        onDelete = { vm.deletePanel(panel.id) },
                    )
                }
            }
        }
    }

    editing?.let { panel ->
        PanelEditorDialog(
            initial = panel,
            presets = state.presets,
            onPreview = vm::preview,
            onSave = { vm.savePanel(it); editing = null },
            onDismiss = { editing = null },
        )
    }
    if (sourceOpen) {
        SourceDialog(
            current = state.config.source,
            discovered = state.discovered,
            discovering = state.discovering,
            discoveryError = state.discoveryError,
            onDiscover = { vm.discover() },
            onTest = vm::test,
            onSave = vm::setSource,
            onDismiss = { sourceOpen = false },
        )
    }
}

@Composable
private fun NoSource(state: MetricsState, modifier: Modifier, onSetUp: () -> Unit, onSearch: () -> Unit) {
    Column(modifier.fillMaxSize().padding(24.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        when {
            state.discovering -> {
                Text(stringResource(R.string.metrics_searching))
                LinearProgressIndicator(Modifier.fillMaxWidth())
            }
            else -> {
                Text(stringResource(R.string.metrics_none_found), style = MaterialTheme.typography.titleMedium)
                MutedText(state.discoveryError ?: state.error ?: stringResource(R.string.metrics_none_found_hint))
                Button(onClick = onSetUp) { Text(stringResource(R.string.metrics_set_up)) }
                androidx.compose.material3.TextButton(onClick = onSearch) { Text(stringResource(R.string.metrics_search_again)) }
            }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun RangeRow(range: MetricsRange, onRange: (MetricsRange) -> Unit) {
    val ranges = MetricsRange.entries
    SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth()) {
        ranges.forEachIndexed { i, r ->
            SegmentedButton(
                selected = r == range,
                onClick = { onRange(r) },
                shape = SegmentedButtonDefaults.itemShape(i, ranges.size),
                icon = {},
            ) { Text(stringResource(r.label)) }
        }
    }
}

@Composable
private fun PanelCard(
    panel: PromPanel,
    result: PanelResult,
    first: Boolean,
    last: Boolean,
    onEdit: () -> Unit,
    onMove: (Int) -> Unit,
    onDelete: () -> Unit,
) {
    var menu by remember { mutableStateOf(false) }
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(start = 16.dp, end = 4.dp, top = 4.dp, bottom = 16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(panel.title, style = MaterialTheme.typography.titleSmall, modifier = Modifier.weight(1f))
                Box {
                    TooltipIconButton(Icons.Outlined.MoreVert, stringResource(R.string.common_more), onClick = { menu = true })
                    DropdownMenu(expanded = menu, onDismissRequest = { menu = false }) {
                        DropdownMenuItem(text = { Text(stringResource(R.string.metrics_edit)) }, onClick = { menu = false; onEdit() })
                        if (!first) DropdownMenuItem(text = { Text(stringResource(R.string.metrics_move_up)) }, onClick = { menu = false; onMove(-1) })
                        if (!last) DropdownMenuItem(text = { Text(stringResource(R.string.metrics_move_down)) }, onClick = { menu = false; onMove(1) })
                        DropdownMenuItem(text = { Text(stringResource(R.string.common_delete)) }, onClick = { menu = false; onDelete() })
                    }
                }
            }
            Column(Modifier.padding(end = 12.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                if (result.loading) LinearProgressIndicator(Modifier.fillMaxWidth())
                result.error?.let { Text(it, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall) }
                result.result?.let { res -> PromChart(panel, res) }
            }
        }
    }
}

/** A result as a chart, with its notes: no data, dropped series, server warnings. */
@Composable
internal fun PromChart(panel: PromPanel, res: name.levis.ichor.model.PromResult) {
    if (res.series.isEmpty()) {
        MutedText(stringResource(R.string.metrics_no_data))
    } else {
        TimeSeriesChart(
            title = panel.title.ifBlank { panel.query },
            times = res.times,
            lines = res.series.map { TimeLine(it.legend(panel.legend), it.values) },
            format = { formatMetric(it, panel.unit) },
        )
    }
    if (res.truncated) MutedText(pluralStringResource(R.plurals.metrics_truncated, res.series.size, res.series.size, res.total))
    res.warnings.forEach { MutedText(it) }
}
