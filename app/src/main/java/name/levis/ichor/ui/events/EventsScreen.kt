package name.levis.ichor.ui.events

import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.rememberScrollState
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
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
import name.levis.ichor.data.OVERVIEW
import name.levis.ichor.model.ClusterOverview
import name.levis.ichor.model.EventFilter
import name.levis.ichor.model.timelineRows
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.LiveIndicator
import name.levis.ichor.ui.factory

/** Machine events of one node ([node]) or of every node of the context ([node] null). */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun EventsScreen(
    node: String?,
    hostname: String?,
    onBack: () -> Unit,
    vm: EventsViewModel = viewModel(
        key = "events-${node ?: "all"}",
        factory = factory { EventsViewModel(app.talosRepository, node) },
    ),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    val lifecycle = LocalLifecycleOwner.current.lifecycle
    // Stream only while visible: leaving the screen or backgrounding the app cancels it.
    LaunchedEffect(lifecycle) { lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) { vm.stream() } }

    val talos = (LocalContext.current.applicationContext as TalosApp).talosRepository
    val hostnames = remember {
        talos.cached<ClusterOverview>(OVERVIEW)?.value?.nodes?.associate { it.node to it.hostname }.orEmpty()
    }
    var filter by rememberSaveable { mutableStateOf(EventFilter.ALL) }
    val rows = remember(state.events, filter) { timelineRows(state.events, filter) }
    // Re-render every 15 s so relative times stay true.
    val now by produceState(System.currentTimeMillis()) {
        while (true) {
            delay(15_000)
            value = System.currentTimeMillis()
        }
    }

    Scaffold(
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text(stringResource(R.string.events_title))
                        Text(
                            hostname ?: stringResource(R.string.events_all_nodes),
                            style = MaterialTheme.typography.labelMedium,
                        )
                    }
                },
                navigationIcon = { BackButton(onBack) },
            )
        },
    ) { padding ->
        Column(Modifier.padding(padding).fillMaxSize()) {
            Row(
                Modifier.horizontalScroll(rememberScrollState()).padding(horizontal = 16.dp),
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                EventFilter.entries.forEach { f ->
                    FilterChip(selected = filter == f, onClick = { filter = f }, label = { Text(stringResource(f.label)) })
                }
            }
            LiveIndicator(state.streaming, state.error, Modifier.padding(horizontal = 16.dp, vertical = 4.dp))
            HorizontalDivider()
            if (rows.isEmpty()) {
                EmptyText(stringResource(if (filter == EventFilter.ALL) R.string.events_empty else R.string.events_empty_filtered))
            } else {
                LazyColumn(Modifier.fillMaxSize()) {
                    itemsIndexed(rows, key = { i, row -> row.event.id.ifEmpty { "#$i" }.let { "${row.event.node}|$it" } }) { _, row ->
                        EventItem(row, hostname = hostnames[row.event.node] ?: row.event.node, showNode = node == null, now = now)
                        HorizontalDivider()
                    }
                }
            }
        }
    }
}

private val EventFilter.label: Int
    get() = when (this) {
        EventFilter.ALL -> R.string.events_filter_all
        EventFilter.PROBLEMS -> R.string.events_filter_problems
        EventFilter.SERVICES -> R.string.events_filter_services
        EventFilter.BOOT -> R.string.events_filter_boot
    }
