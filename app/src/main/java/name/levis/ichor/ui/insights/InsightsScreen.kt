package name.levis.ichor.ui.insights

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import name.levis.ichor.ui.design.IchorButton
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import name.levis.ichor.ui.design.IchorScaffold
import androidx.compose.material3.SecondaryTabRow
import androidx.compose.material3.Text
import name.levis.ichor.ui.design.IchorTextButton
import name.levis.ichor.ui.design.IchorTopAppBar
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.Alignment
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.ui.components.LoadFailureRow
import name.levis.ichor.R
import name.levis.ichor.data.activeSummary
import name.levis.ichor.model.DriftFix
import name.levis.ichor.ui.components.AppTab
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.ConfirmDialog
import name.levis.ichor.ui.components.InfoHint
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionHeader
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.components.KeepScreenOn
import name.levis.ichor.ui.components.SwipeTabPager
import name.levis.ichor.ui.workloads.NetPerfTab
import name.levis.ichor.ui.workloads.netPerfViewModel
import java.text.DateFormat
import name.levis.ichor.util.formatDateTime
import name.levis.ichor.ui.components.pageContent

/** What a confirmed baseline dialog does. */
private enum class BaselineAction { Replace, Delete }

/** Drift, Recorder and NetPerf. */
private val INSIGHTS_TABS = listOf(0, 1, 2)

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun InsightsScreen(
    onBack: () -> Unit,
    /** Opens [node]'s machine config with a draft that undoes a drift difference. */
    onFix: (node: String, hostname: String, fix: DriftFix) -> Unit = { _, _, _ -> },
) {
    val context = LocalContext.current
    val application = context.applicationContext as name.levis.ichor.TalosApp
    val config by application.configRepository.config.collectAsStateWithLifecycle()
    val invalidations by application.talosRepository.invalidations.collectAsStateWithLifecycle()
    val mask by application.uiPreferences.privacyMask.collectAsStateWithLifecycle()
    val cluster = config?.activeSummary?.fingerprint ?: return
    val storageScope = "$cluster-${mask.storageKey}"
    val key = "$storageScope-$invalidations"
    val vm: InsightsViewModel = viewModel(key = key, factory = factory {
        InsightsViewModel(application.talosRepository, InsightsStore(context.applicationContext, storageScope), cluster)
    })
    val netPerf = netPerfViewModel()
    val state by vm.state.collectAsStateWithLifecycle()
    val lifecycle = LocalLifecycleOwner.current.lifecycle
    var tab by rememberSaveable { mutableIntStateOf(0) }
    var metrics by remember { mutableStateOf(false) }
    // A recreated ViewModel has its own baseline: the question does not carry over.
    var confirming by remember(key) { mutableStateOf<BaselineAction?>(null) }
    LaunchedEffect(key) { vm.load() }
    DisposableEffect(vm, lifecycle) {
        val observer = LifecycleEventObserver { _, event -> if (event == Lifecycle.Event.ON_STOP) vm.stop() }
        lifecycle.addObserver(observer)
        onDispose { lifecycle.removeObserver(observer); vm.stop() }
    }
    if (state.recording) KeepScreenOn()
    fun time(at: Long) = formatDateTime(at, DateFormat.SHORT, DateFormat.MEDIUM)
    IchorScaffold(topBar = {
        IchorTopAppBar(title = { Text(stringResource(R.string.insights_title)) }, navigationIcon = {
            BackButton(onBack)
        })
    }) { padding ->
        Column(Modifier.pageContent(padding)) {
            SecondaryTabRow(selectedTabIndex = tab) {
                AppTab(selected = tab == 0, onClick = { tab = 0 }, text = { Text(stringResource(R.string.insights_tab_drift)) })
                AppTab(selected = tab == 1, onClick = { tab = 1 }, text = { Text(stringResource(R.string.insights_tab_recorder)) })
                AppTab(selected = tab == 2, onClick = { tab = 2 }, text = { Text(stringResource(R.string.insights_tab_network)) })
            }
            SwipeTabPager(INSIGHTS_TABS, tab, onSelect = { tab = it }) { page ->
                if (page == 2) {
                    NetPerfTab(netPerf)
                } else {
                    LazyColumn(contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(if (page == 1) 4.dp else 12.dp)) {
                        state.error?.let { item { LoadFailureRow(it, onRetry = { vm.refresh() }) } }
                        if (page == 0) {
                            item {
                                val drift = stringResource(R.string.insights_drift)
                                SectionHeader(
                                    drift,
                                    subtitle = state.snapshot?.let { stringResource(R.string.insights_sample_at, time(it.at)) },
                                    trailing = { InfoHint(drift, stringResource(R.string.insights_drift_note)) },
                                )
                            }
                            item {
                                Column {
                                    Row(horizontalArrangement = Arrangement.spacedBy(12.dp), verticalAlignment = Alignment.CenterVertically) {
                                        IchorButton(onClick = { vm.refresh() }, enabled = !state.busy) { Text(stringResource(R.string.common_refresh)) }
                                        // The first baseline is one tap: there is nothing to lose yet.
                                        IchorTextButton(
                                            onClick = { if (state.baselineAt == null) vm.saveBaseline() else confirming = BaselineAction.Replace },
                                            enabled = state.snapshot != null && !state.busy && !state.baselineBusy,
                                        ) { Text(stringResource(if (state.baselineAt == null) R.string.insights_save_baseline else R.string.insights_replace_baseline)) }
                                    }
                                    state.baselineAt?.let { at ->
                                        Row(verticalAlignment = Alignment.CenterVertically) {
                                            MutedText(stringResource(R.string.insights_baseline_at, time(at)), Modifier.weight(1f))
                                            IchorTextButton(onClick = { confirming = BaselineAction.Delete }, enabled = !state.busy && !state.baselineBusy) {
                                                Text(stringResource(R.string.insights_delete_baseline))
                                            }
                                        }
                                    } ?: MutedText(stringResource(R.string.insights_peer_reference))
                                }
                            }
                            if (state.busy) item { LinearProgressIndicator(Modifier.fillMaxWidth()) }
                            state.snapshot?.let { snapshot ->
                                if (state.changes.isEmpty()) item { Text(stringResource(R.string.insights_no_differences)) }
                                items(state.changes, key = { "${it.node}/${it.key}" }) { change ->
                                    val hostname = change.nodeName.ifEmpty { null }
                                        ?: snapshot.nodes.firstOrNull { it.node == change.node }?.hostname?.ifEmpty { null }
                                        ?: change.node
                                    DriftChangeCard(change, hostname) { fix ->
                                        onFix(change.node, hostname, fix)
                                    }
                                }
                                items(snapshot.nodes.filter { it.errors.isNotEmpty() }, key = { it.node }) { node ->
                                    Text("${node.hostname}: ${node.errors.entries.joinToString { "${it.key}: ${it.value}" }}", color = MaterialTheme.colorScheme.error)
                                }
                            }
                        } else {
                            recorderPage(
                                state = state,
                                metrics = metrics,
                                onMetrics = { metrics = it },
                                onToggleRecording = { if (state.recording) vm.stop() else vm.start() },
                                onDelete = { vm.deleteRecording() },
                                time = ::time,
                            )
                        }
                    }
                }
            }
        }
    }
    val at = state.baselineAt
    val action = confirming
    if (action != null && at != null) {
        val replace = action == BaselineAction.Replace
        ConfirmDialog(
            title = stringResource(if (replace) R.string.insights_replace_baseline_title else R.string.insights_delete_baseline_title),
            text = stringResource(if (replace) R.string.insights_replace_baseline_body else R.string.insights_delete_baseline_body, time(at)),
            confirm = stringResource(if (replace) R.string.insights_replace_baseline else R.string.common_delete),
            onConfirm = {
                confirming = null
                if (replace) vm.saveBaseline() else vm.deleteBaseline()
            },
            onDismiss = { confirming = null },
            destructive = true,
        )
    }
}
