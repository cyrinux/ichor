package name.levis.ichor.ui.argocd

import name.levis.ichor.model.ShareTarget
import name.levis.ichor.ui.share.ShareLinkButton
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.R
import name.levis.ichor.monitor.freezeReminderHook
import name.levis.ichor.TalosApp
import name.levis.ichor.data.OVERVIEW
import name.levis.ichor.model.ArgoAction
import name.levis.ichor.model.ArgoApp
import name.levis.ichor.model.ArgoFreezeAction
import name.levis.ichor.model.ArgoFreezeOptions
import name.levis.ichor.model.ArgoHistory
import name.levis.ichor.model.ArgoProject
import name.levis.ichor.model.ArgoStatus
import name.levis.ichor.model.ArgoSyncOptions
import name.levis.ichor.model.ArgoWindow
import name.levis.ichor.model.ClusterOverview
import name.levis.ichor.model.FREEZE_EXTEND_MINUTES
import name.levis.ichor.model.drifted
import name.levis.ichor.model.freezeWindowsOf
import name.levis.ichor.model.projectOf
import name.levis.ichor.model.KubeWorkload
import name.levis.ichor.model.NodeOverview
import name.levis.ichor.model.shortRevision
import name.levis.ichor.model.waveSteps
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.ConfirmDialog
import name.levis.ichor.ui.components.DataFreshness
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.dataservices.downHostnames
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.workloads.RestartConfirmDialog
import name.levis.ichor.ui.workloads.RestartResultToasts
import name.levis.ichor.ui.workloads.RolloutStatusSheet

/**
 * One Argo CD Application: the hero, its conditions, the running or last sync, the sync-waves
 * timeline with its resources (selective sync, rollout restarts), the pods that are not ready,
 * and the deployment history with rollback. Polls while a sync runs.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ArgoAppScreen(
    namespace: String,
    name: String,
    onBack: () -> Unit,
    onNode: ((NodeOverview, Int) -> Unit)? = null,
    onWindows: (() -> Unit)? = null,
) {
    val talos = LocalContext.current.applicationContext as TalosApp
    val vm: ArgoViewModel = viewModel(factory = factory { ArgoViewModel(talos.talosRepository, freezeReminderHook(talos)) })
    val state by vm.state.collectAsStateWithLifecycle()
    val busy by vm.busy.collectAsStateWithLifecycle()
    val config by talos.configRepository.config.collectAsStateWithLifecycle()
    val generation by talos.configRepository.generation.collectAsStateWithLifecycle()
    val invalidations by talos.talosRepository.invalidations.collectAsStateWithLifecycle()
    LaunchedEffect(config?.activeContext, generation, invalidations) { vm.load(Triple(config?.activeContext, generation, invalidations)) }
    ArgoPolling(vm)
    val snackbar = remember { SnackbarHostState() }
    ArgoActionMessages(vm.results) { snackbar.showSnackbar(it) }
    ArgoFreezeMessages(vm.freezeResults) { snackbar.showSnackbar(it) }
    RestartResultToasts(vm.restarts.results)
    RolloutStatusSheet(vm.restarts)
    val app = (state as? UiState.Loaded)?.data?.apps?.firstOrNull { it.namespace == namespace && it.name == name }

    Scaffold(
        snackbarHost = { SnackbarHost(snackbar) },
        topBar = {
            TopAppBar(
                title = { Text(name) },
                navigationIcon = { BackButton(onBack) },
                actions = {
                    ShareLinkButton(ShareTarget.argoApp(namespace, name))
                    TooltipIconButton(Icons.Outlined.Refresh, stringResource(R.string.common_refresh), onClick = { vm.refresh() })
                },
            )
        },
    ) { padding ->
        val modifier = Modifier.padding(padding)
        when (val s = state) {
            UiState.Loading -> LoadingBox(modifier)
            is UiState.Failed -> ErrorBox(s.message, vm::refresh, modifier)
            is UiState.Loaded -> Column(modifier.fillMaxSize()) {
                PullToRefreshBox(isRefreshing = s.refreshing, onRefresh = vm::refresh, modifier = Modifier.weight(1f)) {
                    if (app == null) {
                        EmptyText(stringResource(R.string.argo_app_gone, name))
                    } else {
                        val overview = remember(s.data) { talos.talosRepository.cached<ClusterOverview>(OVERVIEW)?.value }
                        val downNodes = remember(overview) { overview?.downHostnames().orEmpty() }
                        val network = NetworkContext(s.fetchedAt, downNodes, overview?.nodes.orEmpty(), onNode)
                        val project = s.data.projectOf(app)
                        val freeze = FreezeContext(s.data, project, vm.freezeBusy(busy, project), onWindows)
                        AppDetail(app, downNodes, app.key in busy, vm, network, freeze)
                    }
                }
                DataFreshness(s, edgeToEdge = false)
            }
        }
    }
}

@Composable
private fun AppDetail(app: ArgoApp, downNodes: Set<String>, busy: Boolean, vm: ArgoViewModel, network: NetworkContext, freeze: FreezeContext) {
    var selecting by rememberSaveable { mutableStateOf(false) }
    var freezeSheet by remember { mutableStateOf(false) }
    var endFreeze by remember { mutableStateOf(false) }
    var selected by rememberSaveable { mutableStateOf(setOf<String>()) }
    var sheet by remember { mutableStateOf(false) }
    var terminate by remember { mutableStateOf(false) }
    var rollback by remember { mutableStateOf<ArgoHistory?>(null) }
    var restart by remember { mutableStateOf<KubeWorkload?>(null) }
    val chosen = app.resources.filter { it.key in selected }
    val act = { action: ArgoAction, options: ArgoSyncOptions? -> vm.act(listOf(app), action, options) }

    if (sheet) {
        ArgoSyncSheet(
            app,
            resources = if (selecting) chosen else emptyList(),
            onSync = { options ->
                sheet = false
                selecting = false
                selected = emptySet()
                act(ArgoAction.SYNC, options)
            },
            onDismiss = { sheet = false },
        )
    }
    if (freezeSheet) {
        ArgoFreezeSheet(
            app,
            freeze.status,
            onFreeze = { project, options ->
                freezeSheet = false
                vm.freeze(project, ArgoFreezeAction.FREEZE, listOf(options))
            },
            onPauseAutoSync = if (app.canChangeSpec && app.autoSync.enabled) ({ freezeSheet = false; act(ArgoAction.AUTO_SYNC_OFF, null) }) else null,
            onDismiss = { freezeSheet = false },
        )
    }
    val ichorWindows = freeze.windows(app).filter { it.ichor != null }
    if (endFreeze && freeze.project != null) {
        val project = freeze.project
        val unfreeze = ichorWindows.map { ArgoFreezeOptions(window = it.id) }
        EndFreezeDialog(
            drifted = app.drifted,
            covers = ichorWindows.maxOfOrNull { it.apps } ?: 1,
            onEnd = { endFreeze = false; vm.freeze(project, ArgoFreezeAction.UNFREEZE, unfreeze) },
            onEndAndSync = if (app.outOfSync && !app.isRunning) ({ endFreeze = false; vm.freeze(project, ArgoFreezeAction.UNFREEZE, unfreeze, syncAfter = app) }) else null,
            onDismiss = { endFreeze = false },
        )
    }
    if (terminate) {
        ConfirmDialog(
            title = stringResource(R.string.argo_terminate_title, app.name),
            text = stringResource(R.string.argo_terminate_text),
            confirm = stringResource(R.string.argo_terminate),
            onConfirm = { terminate = false; act(ArgoAction.TERMINATE, null) },
            onDismiss = { terminate = false },
        )
    }
    rollback?.let { h ->
        ConfirmDialog(
            title = stringResource(R.string.argo_rollback_title, shortRevision(h.revision)),
            text = stringResource(R.string.argo_rollback_text, app.name, shortRevision(h.revision)),
            confirm = stringResource(R.string.argo_rollback_confirm),
            onConfirm = { rollback = null; act(ArgoAction.ROLLBACK, ArgoSyncOptions(historyId = h.id)) },
            onDismiss = { rollback = null },
        )
    }
    restart?.let { w ->
        // The replicas decide the downtime warning: read them fresh, unless dismissed meanwhile.
        LaunchedEffect(w.key) {
            val fresh = vm.restarts.current(w)
            if (restart?.key == w.key) restart = fresh
        }
        RestartConfirmDialog(w, onConfirm = { restart = null; vm.restarts.restart(w) }, onDismiss = { restart = null })
    }

    LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(16.dp)) {
        item(key = "hero") {
            ArgoAppHero(app, autoSyncBusy = busy) { on -> act(if (on) ArgoAction.AUTO_SYNC_ON else ArgoAction.AUTO_SYNC_OFF, null) }
        }
        item(key = "actions") {
            ArgoActionButtons(
                busy = busy,
                running = app.isRunning,
                onSync = { sheet = true },
                onRefresh = { act(ArgoAction.REFRESH, null) },
                onHardRefresh = { act(ArgoAction.HARD_REFRESH, null) },
            )
        }
        item(key = "freeze") {
            ArgoFreezeCard(
                app,
                windows = freeze.windows(app),
                busy = freeze.busy,
                onFreeze = { freezeSheet = true },
                onExtend = {
                    // The window ending last is the one "until" shows.
                    ichorWindows.maxByOrNull { it.endsAt }?.let { w ->
                        freeze.project?.let { vm.freeze(it, ArgoFreezeAction.EXTEND, listOf(ArgoFreezeOptions(window = w.id, minutes = FREEZE_EXTEND_MINUTES))) }
                    }
                },
                onUnfreeze = { endFreeze = true },
                onWindows = freeze.onWindows,
            )
        }
        if (app.conditions.isNotEmpty()) item(key = "conditions") { ConditionBanners(app.conditions) }
        app.operation?.let { op -> item(key = "operation") { ArgoOperationCard(app, op, busy) { terminate = true } } }
        item(key = "network") {
            ArgoNetworkSection(app, network.fetchedAt, network.downNodes, network.talosNodes, network.onNode)
        }
        if (app.unhealthyPods.isNotEmpty()) {
            item(key = "pods-title") { SectionTitle(stringResource(R.string.argo_unhealthy_pods)) }
            items(app.unhealthyPods, key = { "pod/${it.key}" }) { UnhealthyPodRow(it, it.node.isNotEmpty() && it.node in downNodes) }
        }
        if (app.resources.isNotEmpty()) {
            item(key = "waves-title") {
                WavesHeader(
                    selecting = selecting,
                    count = chosen.size,
                    onSelecting = { selecting = it; if (!it) selected = emptySet() },
                    onSyncSelected = { sheet = true },
                    enabled = !busy && !app.isRunning,
                )
            }
            item(key = "waves") {
                ArgoWaveTimeline(
                    steps = app.waveSteps(),
                    selecting = selecting,
                    selected = selected,
                    onToggle = { r -> selected = if (r.key in selected) selected - r.key else selected + r.key },
                    onRestart = { r -> restart = vm.workloadFor(r) },
                )
            }
        }
        history(app) { rollback = it }
    }
}

/** What the freeze card needs: the status (projects, other apps), the app's project, whether it is changing. */
private data class FreezeContext(val status: ArgoStatus, val project: ArgoProject?, val busy: Boolean, val onWindows: (() -> Unit)?) {
    fun windows(app: ArgoApp): List<ArgoWindow> = status.freezeWindowsOf(app).map { it.window }
}

/** What the network section needs from the screen: when the app was fetched, and the Talos nodes. */
private data class NetworkContext(
    val fetchedAt: Long,
    val downNodes: Set<String>,
    val talosNodes: List<NodeOverview>,
    val onNode: ((NodeOverview, Int) -> Unit)?,
)

private fun LazyListScope.history(app: ArgoApp, onRollback: (ArgoHistory) -> Unit) {
    if (app.history.isEmpty()) return
    item(key = "history-title") { SectionTitle(stringResource(R.string.argo_history)) }
    item(key = "history") {
        Column {
            app.history.forEachIndexed { i, h ->
                // The newest entry is what runs: rolling back to it would change nothing.
                HistoryRow(h, current = i == 0, last = i == app.history.lastIndex, onRollback = if (i > 0 && app.canRollback) ({ onRollback(h) }) else null)
            }
            // Why rolling back is not offered; nothing to say while a sync runs.
            val rollbackHint = when {
                !app.canChangeSpec -> R.string.argo_rollback_owned
                app.autoSync.enabled -> R.string.argo_rollback_needs_pause
                else -> null
            }
            if (!app.canRollback && app.history.size > 1 && rollbackHint != null) {
                MutedText(stringResource(rollbackHint))
            }
        }
    }
}

@Composable
private fun WavesHeader(selecting: Boolean, count: Int, onSelecting: (Boolean) -> Unit, onSyncSelected: () -> Unit, enabled: Boolean) {
    Row(verticalAlignment = Alignment.CenterVertically) {
        SectionTitle(stringResource(R.string.argo_waves), Modifier.weight(1f))
        if (selecting) {
            TextButton(onClick = { onSelecting(false) }) { Text(stringResource(R.string.common_cancel)) }
            FilledTonalButton(onClick = onSyncSelected, enabled = enabled && count > 0) { Text(stringResource(R.string.argo_sync_selected, count)) }
        } else {
            TextButton(onClick = { onSelecting(true) }, enabled = enabled) { Text(stringResource(R.string.argo_select)) }
        }
    }
}
