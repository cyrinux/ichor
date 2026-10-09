package name.levis.ichor.ui.argocd

import name.levis.ichor.model.KubeAction
import name.levis.ichor.ui.components.rememberKubeDenial
import name.levis.ichor.model.ShareTarget
import name.levis.ichor.ui.share.ShareLinkButton
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.AcUnit
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material.icons.outlined.Sync
import androidx.compose.material3.BottomAppBar
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.PrimaryTabRow
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import name.levis.ichor.ui.components.AppTab
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
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
import name.levis.ichor.model.ArgoStatus
import name.levis.ichor.model.ClusterOverview
import name.levis.ichor.model.FreezeScope
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.ConfirmDialog
import name.levis.ichor.ui.components.DataFreshness
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.dataservices.downHostnames
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.components.pageContent

/**
 * Argo CD's Applications (first tab) and its ApplicationSets and projects (second), read through
 * its custom resources with the admin kubeconfig Talos issues (os:admin). Polls while a sync
 * runs; [onApp] opens an app (namespace, name).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ArgoAppsScreen(onBack: () -> Unit, onApp: (namespace: String, name: String) -> Unit, onWindows: () -> Unit) {
    val app = LocalContext.current.applicationContext as TalosApp
    val vm: ArgoViewModel = viewModel(factory = factory { ArgoViewModel(app.gitOpsRepository, app.kubeRepository, freezeReminderHook(app)) })
    val state by vm.state.collectAsStateWithLifecycle()
    val busy by vm.busy.collectAsStateWithLifecycle()
    val config by app.configRepository.config.collectAsStateWithLifecycle()
    val generation by app.configRepository.generation.collectAsStateWithLifecycle()
    val invalidations by app.talosRepository.invalidations.collectAsStateWithLifecycle()
    LaunchedEffect(config?.activeContext, generation, invalidations) { vm.load(Triple(config?.activeContext, generation, invalidations)) }
    ArgoPolling(vm)

    val snackbar = remember { SnackbarHostState() }
    ArgoActionMessages(vm.results) { snackbar.showSnackbar(it) }
    ArgoFreezeMessages(vm.freezeResults) { snackbar.showSnackbar(it) }
    var freezing by remember { mutableStateOf<Pair<ArgoApp, FreezeScope>?>(null) }
    var selection by rememberSaveable { mutableStateOf(setOf<String>()) }
    var tab by rememberSaveable { mutableIntStateOf(0) }
    var confirmSync by remember { mutableStateOf<List<ArgoApp>?>(null) }
    val loaded = (state as? UiState.Loaded)?.data
    val selectedApps = loaded?.apps.orEmpty().filter { it.key in selection }

    confirmSync?.let { apps ->
        SyncManyDialog(
            apps,
            onConfirm = {
                confirmSync = null
                selection = emptySet()
                vm.act(apps, ArgoAction.SYNC)
            },
            onDismiss = { confirmSync = null },
        )
    }

    val loadedForFreeze = loaded
    freezing?.let { (anchor, scope) ->
        if (loadedForFreeze != null) {
            ArgoFreezeSheet(
                anchor,
                loadedForFreeze,
                initialScope = scope,
                onFreeze = { project, options ->
                    freezing = null
                    vm.freeze(project, ArgoFreezeAction.FREEZE, listOf(options))
                },
                onPauseAutoSync = null,
                onDismiss = { freezing = null },
            )
        }
    }

    Scaffold(
        snackbarHost = { SnackbarHost(snackbar) },
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text(stringResource(R.string.argo_title))
                        loaded?.let { Subtitle(it) }
                    }
                },
                navigationIcon = { BackButton(onBack) },
                actions = {
                    ShareLinkButton(ShareTarget.screen(ShareTarget.ARGO_CD))
                    TooltipIconButton(Icons.Outlined.AcUnit, stringResource(R.string.argo_windows_title), onClick = onWindows)
                    TooltipIconButton(Icons.Outlined.Refresh, stringResource(R.string.common_refresh), onClick = { vm.refresh() })
                },
            )
        },
        bottomBar = {
            if (selection.isNotEmpty()) {
                SelectionBar(
                    count = selectedApps.size,
                    enabled = rememberKubeDenial(KubeAction.ARGO_SYNC, selectedApps.firstOrNull()?.namespace.orEmpty()) == null,
                    onClear = { selection = emptySet() },
                    onSync = { confirmSync = selectedApps.filterNot { it.isRunning }.takeIf { it.isNotEmpty() } },
                    onRefresh = {
                        vm.act(selectedApps, ArgoAction.REFRESH)
                        selection = emptySet()
                    },
                )
            }
        },
    ) { padding ->
        val modifier = Modifier.pageContent(padding)
        when (val s = state) {
            UiState.Loading -> LoadingBox(modifier)
            is UiState.Failed -> ErrorBox(s.message, vm::refresh, modifier)
            is UiState.Loaded -> {
                val downNodes = remember(s.data) {
                    app.talosRepository.cached<ClusterOverview>(OVERVIEW)?.value?.downHostnames().orEmpty()
                }
                Column(modifier.fillMaxSize()) {
                    if (!s.data.installed && s.data.apps.isEmpty()) {
                        EmptyText(stringResource(R.string.argo_not_installed))
                        return@Column
                    }
                    PrimaryTabRow(selectedTabIndex = tab) {
                        AppTab(selected = tab == 0, onClick = { tab = 0 }, text = { Text(stringResource(R.string.argo_tab_apps)) })
                        AppTab(selected = tab == 1, onClick = { tab = 1 }, text = { Text(stringResource(R.string.argo_tab_sets)) })
                    }
                    PullToRefreshBox(isRefreshing = s.refreshing, onRefresh = vm::refresh, modifier = Modifier.weight(1f)) {
                        if (tab == 0) {
                            ArgoAppsTab(
                                status = s.data,
                                downNodes = downNodes,
                                busy = busy,
                                selection = selection,
                                onSelection = { selection = it },
                                onOpen = { onApp(it.namespace, it.name) },
                                onAct = { a, action -> vm.act(listOf(a), action) },
                                onSyncAll = { confirmSync = it },
                                onFreeze = { a, scope -> freezing = a to scope },
                            )
                        } else {
                            ArgoSetsTab(s.data, onWindows)
                        }
                    }
                    DataFreshness(s, edgeToEdge = false)
                }
            }
        }
    }
}

/** "v3.4.5 · 8 apps". */
@Composable
private fun Subtitle(status: ArgoStatus) {
    Text(
        listOf(status.version, pluralStringResource(R.plurals.argo_apps, status.apps.size, status.apps.size)).filter { it.isNotEmpty() }.joinToString(" · "),
        style = MaterialTheme.typography.labelMedium,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        maxLines = 1,
        overflow = TextOverflow.Ellipsis,
    )
}

@Composable
private fun SelectionBar(count: Int, enabled: Boolean, onClear: () -> Unit, onSync: () -> Unit, onRefresh: () -> Unit) {
    BottomAppBar(
        actions = {
            IconButton(onClick = onClear) { Icon(Icons.Outlined.Close, stringResource(R.string.argo_clear_selection)) }
            Text(pluralStringResource(R.plurals.argo_selected, count, count), style = MaterialTheme.typography.titleSmall)
        },
        floatingActionButton = {
            Row {
                OutlinedButton(onClick = onRefresh, enabled = count > 0 && enabled) {
                    Icon(Icons.Outlined.Refresh, contentDescription = null, modifier = Modifier.size(18.dp))
                    Text(stringResource(R.string.argo_refresh), modifier = Modifier.padding(start = 6.dp))
                }
                FilledTonalButton(onClick = onSync, enabled = count > 0 && enabled, modifier = Modifier.padding(start = 8.dp)) {
                    Icon(Icons.Outlined.Sync, contentDescription = null, modifier = Modifier.size(18.dp))
                    Text(stringResource(R.string.argo_sync), modifier = Modifier.padding(start = 6.dp))
                }
            }
        },
    )
}

/** Syncing several apps at once: names them, says nothing gets pruned. */
@Composable
private fun SyncManyDialog(apps: List<ArgoApp>, onConfirm: () -> Unit, onDismiss: () -> Unit) {
    ConfirmDialog(
        title = pluralStringResource(R.plurals.argo_sync_many_title, apps.size, apps.size),
        text = apps.take(MAX_NAMED).joinToString(", ") { it.name } + (if (apps.size > MAX_NAMED) ", …" else "") +
            "\n\n" + stringResource(R.string.argo_sync_many_text),
        confirm = stringResource(R.string.argo_sync),
        onConfirm = onConfirm,
        onDismiss = onDismiss,
    )
}

private const val MAX_NAMED = 8
