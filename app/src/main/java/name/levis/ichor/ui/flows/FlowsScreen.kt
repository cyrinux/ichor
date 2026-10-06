package name.levis.ichor.ui.flows

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.ArrowDropDown
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.InputChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedCard
import androidx.compose.material3.PrimaryTabRow
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Surface
import androidx.compose.material3.Tab
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.repeatOnLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.R
import name.levis.ichor.model.CiliumStatus
import name.levis.ichor.model.HUBBLE_NODE_ERROR
import name.levis.ichor.model.HUBBLE_NODE_LIVE
import name.levis.ichor.model.HubbleFilter
import name.levis.ichor.model.HubbleSnapshot
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.InfoBox
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.LiveIndicator
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SpinnerBox
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.components.SwipeTabPager
import name.levis.ichor.ui.components.TWO_TABS

/**
 * The cluster's network flows live through Hubble, like Hubble UI: drops grouped by endpoints
 * and port with the policies behind them, and every flow. Streams while on screen; leaving or
 * backgrounding stops it, and a new filter restarts it. [namespace] and [pod] narrow it from
 * the start (a pod's "Live flows").
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun FlowsScreen(
    onBack: () -> Unit,
    namespace: String? = null,
    pod: String? = null,
    vm: FlowsViewModel = viewModel(
        key = "flows-$namespace-$pod",
        factory = factory { FlowsViewModel(app.ciliumRepository, HubbleFilter(namespace, pod?.takeIf { namespace != null })) },
    ),
) {
    val status by vm.state.collectAsStateWithLifecycle()
    val filter by vm.filter.collectAsStateWithLifecycle()
    val flows by vm.flows.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { if (status == UiState.Loading) vm.refresh() }
    val ready = (status as? UiState.Loaded)?.data?.let { it.installed && it.hubble } == true
    val lifecycle = LocalLifecycleOwner.current.lifecycle
    // Stream only while visible: leaving, backgrounding or a new filter cancels it.
    LaunchedEffect(ready, filter, lifecycle) {
        if (ready) lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) { vm.stream(filter) }
    }

    Scaffold(
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text(stringResource(R.string.flows_title))
                        Text(
                            filter.pod?.let { "${filter.namespace}/$it" } ?: filter.namespace ?: stringResource(R.string.workloads_all_namespaces),
                            style = MaterialTheme.typography.labelMedium,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                        )
                    }
                },
                navigationIcon = { BackButton(onBack) },
            )
        },
        bottomBar = {
            if (ready) {
                Surface(color = MaterialTheme.colorScheme.surfaceContainer, modifier = Modifier.fillMaxWidth()) {
                    LiveIndicator(flows.streaming, flows.error, Modifier.navigationBarsPadding().padding(horizontal = 16.dp, vertical = 6.dp))
                }
            }
        },
    ) { padding ->
        val modifier = Modifier.padding(padding)
        when (val s = status) {
            UiState.Loading -> LoadingBox(modifier)
            is UiState.Failed -> ErrorBox(s.message, vm::refresh, modifier)
            is UiState.Loaded -> when {
                !s.data.installed -> InfoBox(stringResource(R.string.flows_not_installed), modifier, onRetry = vm::refresh)
                !s.data.hubble -> HubbleOff(s.data, modifier)
                else -> Column(modifier.fillMaxSize()) {
                    FilterBar(vm, filter)
                    HorizontalDivider()
                    Flows(vm, flows)
                }
            }
        }
    }
}

/** Cilium runs without Hubble: what to turn on. */
@Composable
private fun HubbleOff(status: CiliumStatus, modifier: Modifier) {
    Box(modifier.fillMaxSize().padding(16.dp)) {
        OutlinedCard(Modifier.fillMaxWidth()) {
            Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(stringResource(R.string.flows_hubble_off_title), style = MaterialTheme.typography.titleMedium)
                Text(stringResource(R.string.flows_hubble_off), style = MaterialTheme.typography.bodyMedium)
                if (status.version.isNotEmpty()) MutedText("Cilium ${status.version}")
            }
        }
    }
}

/** Drops only, the namespace (picked from those seen), and the pod when one was given. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun FilterBar(vm: FlowsViewModel, filter: HubbleFilter) {
    val namespaces by vm.namespaces.collectAsStateWithLifecycle()
    var picking by remember { mutableStateOf(false) }
    FlowRow(
        Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        FilterChip(
            selected = filter.dropsOnly,
            onClick = { vm.setFilter(filter.copy(dropsOnly = !filter.dropsOnly)) },
            label = { Text(stringResource(R.string.flows_drops_only)) },
        )
        Box {
            FilterChip(
                selected = filter.namespace != null,
                onClick = {
                    vm.loadPolicies() // its namespaces join the list
                    picking = true
                },
                label = { Text(filter.namespace ?: stringResource(R.string.workloads_all_namespaces), fontFamily = filter.namespace?.let { FontFamily.Monospace }) },
                trailingIcon = { Icon(Icons.Outlined.ArrowDropDown, contentDescription = null, modifier = Modifier.size(18.dp)) },
            )
            DropdownMenu(expanded = picking, onDismissRequest = { picking = false }) {
                DropdownMenuItem(
                    text = { Text(stringResource(R.string.workloads_all_namespaces)) },
                    onClick = {
                        picking = false
                        vm.setFilter(filter.copy(namespace = null, pod = null))
                    },
                )
                namespaces.sorted().forEach { ns ->
                    DropdownMenuItem(
                        text = { Text(ns, fontFamily = FontFamily.Monospace) },
                        onClick = {
                            picking = false
                            vm.setFilter(filter.copy(namespace = ns, pod = filter.pod.takeIf { ns == filter.namespace }))
                        },
                    )
                }
            }
        }
        filter.pod?.let { pod ->
            InputChip(
                selected = true,
                onClick = { vm.setFilter(filter.copy(pod = null)) },
                label = { Text(pod, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis) },
                trailingIcon = { Icon(Icons.Outlined.Close, stringResource(R.string.flows_clear_pod), Modifier.size(18.dp)) },
            )
        }
    }
}

@Composable
private fun Flows(vm: FlowsViewModel, state: FlowsState) {
    var tab by rememberSaveable { mutableIntStateOf(0) }
    var openDrop by rememberSaveable { mutableStateOf<String?>(null) }
    val snapshot = state.snapshot

    openDrop?.let { key ->
        // Kept open with what it last showed if the group falls out of the snapshot.
        val group = snapshot?.drops?.firstOrNull { it.key == key }
        val last = remember(key) { mutableStateOf(group) }
        if (group != null) last.value = group
        last.value?.let { DropDetailSheet(it, vm, onDismiss = { openDrop = null }) }
    }

    Column(Modifier.fillMaxSize()) {
        PrimaryTabRow(selectedTabIndex = tab) {
            Tab(selected = tab == 0, onClick = { tab = 0 }, text = { Text(stringResource(R.string.flows_tab_drops)) })
            Tab(selected = tab == 1, onClick = { tab = 1 }, text = { Text(stringResource(R.string.flows_tab_all)) })
        }
        SwipeTabPager(TWO_TABS, tab, onSelect = { tab = it }) { page ->
            LazyColumn(Modifier.fillMaxSize()) {
                item(key = "status") { StreamStatus(snapshot) }
                when {
                    snapshot == null -> item(key = "waiting") { if (state.streaming) SpinnerBox(Modifier.padding(32.dp)) else EmptyText(stringResource(R.string.flows_no_flows)) }
                    page == 0 && snapshot.drops.isEmpty() -> item(key = "none") { EmptyText(stringResource(R.string.flows_no_drops)) }
                    page == 0 -> items(snapshot.drops, key = { "drop-" + it.key }) { group ->
                        DropCard(group, onClick = { openDrop = group.key })
                    }
                    snapshot.flows.isEmpty() -> item(key = "none") { EmptyText(stringResource(R.string.flows_no_flows)) }
                    else -> items(snapshot.flows.size, key = { "flow-$it" }) { i ->
                        FlowLine(snapshot.flows[i])
                        HorizontalDivider()
                    }
                }
            }
        }
    }
}

/** The agents and their state, the counters, and how far back the history goes. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun StreamStatus(snapshot: HubbleSnapshot?) {
    val colors = LocalStatusColors.current
    Column(Modifier.padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
        if (snapshot == null) return@Column
        FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            snapshot.nodes.forEach { n ->
                val (label, color) = when (n.state) {
                    HUBBLE_NODE_LIVE -> stringResource(R.string.flows_agent_live) to colors.ok
                    HUBBLE_NODE_ERROR -> stringResource(R.string.flows_agent_error) to colors.bad
                    else -> stringResource(R.string.flows_agent_connecting) to colors.warn
                }
                StatusPill("${n.node} · $label", color)
            }
        }
        snapshot.nodes.filter { it.error.isNotEmpty() }.forEach { InlineError("${it.node}: ${it.error}") }
        Text(
            listOfNotNull(
                stringResource(R.string.flows_seen, snapshot.seen),
                stringResource(R.string.flows_dropped, snapshot.dropped),
                snapshot.lost.takeIf { it > 0 }?.let { stringResource(R.string.flows_lost, it) },
            ).joinToString("  ·  "),
            style = MaterialTheme.typography.bodyMedium,
        )
        if (snapshot.buffer > 0) MutedText(stringResource(R.string.flows_history_note, snapshot.buffer))
        if (snapshot.policiesError.isNotEmpty()) InlineError(stringResource(R.string.flows_policies_error, snapshot.policiesError))
    }
}
