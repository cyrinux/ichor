package name.levis.ichor.ui.workloads

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import name.levis.ichor.R
import name.levis.ichor.model.NETPERF_DURATIONS
import name.levis.ichor.model.NETPERF_PHASE_CLEANING
import name.levis.ichor.model.NETPERF_PHASE_STARTING
import name.levis.ichor.model.NETPERF_PHASE_TESTING
import name.levis.ichor.model.NetPerfNode
import name.levis.ichor.model.NetPerfSetup
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.InfoHint
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.KeepScreenOn
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * A network test between two nodes (netperf, like `cilium connectivity perf`): the setup,
 * then its progress and the measurements. Leaving the screen stops a running test.
 */
@Composable
fun NetPerfTab(vm: NetPerfViewModel, modifier: Modifier = Modifier) {
    val state by vm.state.collectAsStateWithLifecycle()
    val setup by vm.setup.collectAsStateWithLifecycle()
    val session by vm.session.collectAsStateWithLifecycle()
    val history by vm.history.collectAsStateWithLifecycle()
    val viewing by vm.viewing.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { if (state == UiState.Loading) vm.refresh() }
    var confirming by remember { mutableStateOf(false) }

    if (confirming) {
        NetPerfConfirmDialog(
            setup = setup,
            onConfirm = {
                confirming = false
                vm.start()
            },
            onDismiss = { confirming = false },
        )
    }

    when (val s = state) {
        UiState.Loading -> LoadingBox(modifier)
        is UiState.Failed -> ErrorBox(s.message, vm::refresh, modifier)
        is UiState.Loaded -> {
            LaunchedEffect(s.data) { vm.nodesLoaded(s.data) }
            Column(
                modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(vertical = 12.dp),
                verticalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                val current = session
                val open = viewing
                if (current == null && open != null) {
                    NetPerfSaved(open, onBack = vm::close, onDelete = { vm.delete(open) })
                } else if (current == null) {
                    NetPerfSetupForm(s.data, setup, vm::update, onStart = { confirming = true })
                    NetPerfTrend(history, setup.client, setup.server)
                    NetPerfHistoryList(history, onOpen = vm::open)
                } else {
                    if (current.running) KeepScreenOn()
                    NetPerfStatus(current, onStop = vm::stop, onReset = vm::reset)
                    NetPerfResults(current.setup, current.results, current.running)
                    current.report?.cleanup?.takeIf { it.isNotEmpty() }?.let {
                        Text(
                            stringResource(R.string.netperf_cleanup_failed, it),
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            modifier = Modifier.padding(horizontal = 16.dp),
                        )
                    }
                }
            }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun NetPerfSetupForm(nodes: List<NetPerfNode>, setup: NetPerfSetup, onChange: ((NetPerfSetup) -> NetPerfSetup) -> Unit, onStart: () -> Unit) {
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    val gutter = Modifier.padding(horizontal = 16.dp)
    Text(stringResource(R.string.netperf_intro), style = MaterialTheme.typography.bodySmall, color = muted, modifier = gutter)
    if (nodes.none { it.ready }) {
        Text(stringResource(R.string.netperf_no_nodes), color = muted, modifier = gutter)
        return
    }
    NodeChips(
        stringResource(R.string.netperf_client),
        nodes,
        setup.client,
        hint = { InfoHint(stringResource(R.string.netperf_hint_nodes_title), stringResource(R.string.netperf_hint_nodes)) },
    ) { name -> onChange { it.copy(client = name) } }
    NodeChips(stringResource(R.string.netperf_server), nodes, setup.server) { name -> onChange { it.copy(server = name) } }
    if (setup.ready && setup.server == setup.client) {
        Text(stringResource(R.string.netperf_same_node), style = MaterialTheme.typography.bodySmall, color = LocalStatusColors.current.warn, modifier = gutter)
    }
    Row(gutter.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f)) {
            Text(stringResource(R.string.netperf_host), style = MaterialTheme.typography.bodyMedium)
            Text(stringResource(R.string.netperf_host_desc), style = MaterialTheme.typography.bodySmall, color = muted)
        }
        InfoHint(stringResource(R.string.netperf_hint_paths_title), stringResource(R.string.netperf_hint_paths))
        Switch(checked = setup.hostNetwork, onCheckedChange = { on -> onChange { it.copy(hostNetwork = on) } }, modifier = Modifier.padding(start = 12.dp))
    }
    Row(gutter, verticalAlignment = Alignment.CenterVertically) {
        Text(stringResource(R.string.netperf_duration), style = MaterialTheme.typography.labelLarge)
        InfoHint(stringResource(R.string.netperf_duration), stringResource(R.string.netperf_hint_duration))
    }
    SingleChoiceSegmentedButtonRow(gutter.fillMaxWidth()) {
        NETPERF_DURATIONS.forEachIndexed { index, seconds ->
            SegmentedButton(
                selected = seconds == setup.seconds,
                onClick = { onChange { it.copy(seconds = seconds) } },
                shape = SegmentedButtonDefaults.itemShape(index, NETPERF_DURATIONS.size),
            ) { Text(stringResource(R.string.netperf_seconds, seconds)) }
        }
    }
    Button(onClick = onStart, enabled = setup.ready, modifier = gutter.fillMaxWidth()) { Text(stringResource(R.string.netperf_start)) }
}

@Composable
private fun NodeChips(
    label: String,
    nodes: List<NetPerfNode>,
    selected: String,
    hint: @Composable () -> Unit = {},
    onSelect: (String) -> Unit,
) {
    Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Row(Modifier.padding(horizontal = 16.dp), verticalAlignment = Alignment.CenterVertically) {
            Text(label, style = MaterialTheme.typography.labelLarge)
            hint()
        }
        LazyRow(contentPadding = PaddingValues(horizontal = 16.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            items(nodes, key = { it.name }) { node ->
                FilterChip(
                    selected = node.name == selected,
                    onClick = { onSelect(node.name) },
                    enabled = node.ready,
                    label = { Text(node.name, fontFamily = FontFamily.Monospace) },
                )
            }
        }
    }
}

@Composable
private fun NetPerfStatus(session: NetPerfSession, onStop: () -> Unit, onReset: () -> Unit) {
    val gutter = Modifier.padding(horizontal = 16.dp)
    val setup = session.setup
    Text(
        stringResource(R.string.netperf_summary, setup.client, setup.server, setup.seconds),
        style = MaterialTheme.typography.titleSmall,
        fontFamily = FontFamily.Monospace,
        modifier = gutter,
    )
    when {
        session.running -> {
            val progress = session.progress
            val done = session.results.size
            LinearProgressIndicator(progress = { done.toFloat() / setup.steps }, modifier = gutter.fillMaxWidth())
            Text(
                if (session.stopping) stringResource(R.string.netperf_stopping) else phaseText(session),
                style = MaterialTheme.typography.bodyMedium,
                modifier = gutter,
            )
            if (progress?.phase != NETPERF_PHASE_CLEANING) {
                MutedText(stringResource(R.string.netperf_keep_open), modifier = gutter)
            }
            OutlinedButton(onClick = onStop, enabled = !session.stopping, modifier = gutter.fillMaxWidth()) { Text(stringResource(R.string.netperf_stop)) }
        }
        else -> {
            val context = LocalContext.current
            session.error?.let { InlineError(stringResource(R.string.netperf_failed, it.resolve(context)), gutter) }
            if (session.stopped) Text(stringResource(R.string.netperf_stopped), modifier = gutter)
            OutlinedButton(onClick = onReset, modifier = gutter.fillMaxWidth()) { Text(stringResource(R.string.netperf_again)) }
        }
    }
}

@Composable
private fun phaseText(session: NetPerfSession): String {
    val p = session.progress ?: return stringResource(R.string.netperf_phase_preparing)
    return when (p.phase) {
        NETPERF_PHASE_STARTING -> if (p.message.contains(':')) {
            stringResource(R.string.netperf_phase_waiting, p.message)
        } else {
            stringResource(R.string.netperf_phase_starting, session.setup.server)
        }
        NETPERF_PHASE_TESTING -> stringResource(R.string.netperf_phase_testing, pathLabel(p.path), testLabel(p.test), p.step, p.steps)
        NETPERF_PHASE_CLEANING -> stringResource(R.string.netperf_phase_cleaning)
        else -> stringResource(R.string.netperf_phase_preparing)
    }
}

@Composable
private fun NetPerfConfirmDialog(setup: NetPerfSetup, onConfirm: () -> Unit, onDismiss: () -> Unit) {
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.netperf_confirm_title)) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(stringResource(R.string.netperf_confirm_text, setup.client, setup.server, setup.seconds * setup.steps))
                if (setup.hostNetwork) Text(stringResource(R.string.netperf_confirm_host), color = LocalStatusColors.current.warn)
            }
        },
        confirmButton = { TextButton(onClick = onConfirm) { Text(stringResource(R.string.netperf_confirm)) } },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}
