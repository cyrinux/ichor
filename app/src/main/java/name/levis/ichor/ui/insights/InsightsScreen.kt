package name.levis.ichor.ui.insights

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.R
import name.levis.ichor.data.activeSummary
import name.levis.ichor.ui.app
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.components.KeepScreenOn
import name.levis.ichor.ui.workloads.NetPerfTab
import name.levis.ichor.ui.workloads.NetPerfViewModel
import java.text.DateFormat
import java.util.Date

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun InsightsScreen(onBack: () -> Unit) {
    val context = LocalContext.current
    val application = context.applicationContext as name.levis.ichor.TalosApp
    val config by application.configRepository.config.collectAsStateWithLifecycle()
    val invalidations by application.talosRepository.invalidations.collectAsStateWithLifecycle()
    val mask by application.uiPreferences.privacyMask.collectAsStateWithLifecycle()
    val cluster = config?.activeSummary?.fingerprint ?: return
    val maskKey = if (mask.enabled) java.security.MessageDigest.getInstance("SHA-256").digest(mask.words.toByteArray()).take(8).joinToString("") { "%02x".format(it) } else "real"
    val storageScope = "$cluster-$maskKey"
    val key = "$storageScope-$invalidations"
    val vm: InsightsViewModel = viewModel(key = key, factory = factory {
        InsightsViewModel(application.talosRepository, InsightsStore(context.applicationContext, storageScope), cluster)
    })
    // Per cluster, so another cluster's nodes never show; leaving the screen stops a running test.
    val netPerf: NetPerfViewModel = viewModel(key = "netperf-$cluster", factory = factory { NetPerfViewModel(application.netPerfRepository) })
    val state by vm.state.collectAsStateWithLifecycle()
    val lifecycle = LocalLifecycleOwner.current.lifecycle
    var tab by remember { mutableIntStateOf(0) }
    var metrics by remember { mutableStateOf(false) }
    LaunchedEffect(key) { vm.load() }
    DisposableEffect(vm, lifecycle) {
        val observer = LifecycleEventObserver { _, event -> if (event == Lifecycle.Event.ON_STOP) vm.stop() }
        lifecycle.addObserver(observer)
        onDispose { lifecycle.removeObserver(observer); vm.stop() }
    }
    if (state.recording) KeepScreenOn()
    fun time(at: Long) = DateFormat.getDateTimeInstance(DateFormat.SHORT, DateFormat.MEDIUM).format(Date(at))
    Scaffold(topBar = {
        TopAppBar(title = { Text(stringResource(R.string.insights_title)) }, navigationIcon = {
            IconButton(onClick = onBack) { Icon(Icons.AutoMirrored.Filled.ArrowBack, stringResource(R.string.common_back)) }
        })
    }) { padding ->
        Column(Modifier.padding(padding)) {
            SecondaryTabRow(selectedTabIndex = tab) {
                Tab(selected = tab == 0, onClick = { tab = 0 }, text = { Text(stringResource(R.string.insights_drift)) })
                Tab(selected = tab == 1, onClick = { tab = 1 }, text = { Text(stringResource(R.string.insights_recorder)) })
                Tab(selected = tab == 2, onClick = { tab = 2 }, text = { Text(stringResource(R.string.netperf_tab)) })
            }
            if (tab == 2) {
                NetPerfTab(netPerf)
                return@Column
            }
            LazyColumn(contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                state.error?.let { item { Text(it, color = MaterialTheme.colorScheme.error) } }
                if (tab == 0) {
                    item { Text(stringResource(R.string.insights_drift_note), style = MaterialTheme.typography.bodySmall) }
                    item {
                        Column {
                            Button(onClick = { vm.refresh() }, enabled = !state.busy) { Text(stringResource(R.string.common_refresh)) }
                            TextButton(onClick = { vm.saveBaseline() }, enabled = state.snapshot != null && !state.busy) { Text(stringResource(R.string.insights_save_baseline)) }
                            state.baselineAt?.let { at ->
                                Text(stringResource(R.string.insights_baseline_at, time(at)))
                                TextButton(onClick = { vm.deleteBaseline() }) { Text(stringResource(R.string.insights_delete_baseline)) }
                            } ?: Text(stringResource(R.string.insights_peer_reference))
                        }
                    }
                    if (state.busy) item { LinearProgressIndicator(Modifier.fillMaxWidth()) }
                    state.snapshot?.let { snapshot ->
                        item { Text(stringResource(R.string.insights_sample_at, time(snapshot.at)), style = MaterialTheme.typography.bodySmall) }
                        if (state.changes.isEmpty()) item { Text(stringResource(R.string.insights_no_differences)) }
                        items(state.changes, key = { "${it.node}/${it.key}" }) { change ->
                            Card(Modifier.fillMaxWidth()) {
                                Column(Modifier.padding(16.dp)) {
                                    Text("${change.node} · ${change.key}", style = MaterialTheme.typography.titleSmall)
                                    Text(stringResource(R.string.insights_reference, change.reference))
                                    Text("${change.before.ifEmpty { "∅" }} → ${change.after.ifEmpty { "∅" }}", fontFamily = FontFamily.Monospace)
                                }
                            }
                        }
                        items(snapshot.nodes.filter { it.errors.isNotEmpty() }, key = { it.node }) { node ->
                            Text("${node.hostname}: ${node.errors.entries.joinToString { "${it.key}: ${it.value}" }}", color = MaterialTheme.colorScheme.error)
                        }
                    }
                } else {
                    item { Text(stringResource(R.string.insights_record_note), style = MaterialTheme.typography.bodySmall) }
                    item {
                        Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                            Button(onClick = { if (state.recording) vm.stop() else vm.start() }) {
                                Text(stringResource(if (state.recording) R.string.insights_stop else R.string.insights_start))
                            }
                            TextButton(onClick = { vm.deleteRecording() }, enabled = !state.recording && state.document != null) { Text(stringResource(R.string.common_delete)) }
                        }
                    }
                    if (state.recording) item { LinearProgressIndicator(Modifier.fillMaxWidth()) }
                    state.document?.let { document ->
                        item { Text("${time(document.startedAt)} — ${time(document.updatedAt)}") }
                        if (document.dropped > 0) item { Text(stringResource(R.string.insights_dropped, document.dropped)) }
                        item {
                            Row {
                                Checkbox(checked = metrics, onCheckedChange = { metrics = it })
                                Text(stringResource(R.string.insights_show_metrics), modifier = Modifier.padding(top = 12.dp))
                            }
                        }
                        items(document.entries.asReversed().filter { metrics || it.kind != "metrics" }, key = { it.id }) { entry ->
                            IncidentEvidenceCard(entry, time(entry.at))
                        }
                    } ?: item { Text(stringResource(R.string.insights_no_recording)) }
                }
            }
        }
    }
}
