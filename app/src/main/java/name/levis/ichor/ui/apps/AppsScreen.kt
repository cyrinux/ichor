package name.levis.ichor.ui.apps

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.OVERVIEW
import name.levis.ichor.data.activeSummary
import name.levis.ichor.model.ClusterOverview
import name.levis.ichor.model.Feature
import name.levis.ichor.model.allows
import name.levis.ichor.model.Inventory
import name.levis.ichor.model.KubeWorkload
import name.levis.ichor.model.NodeOverview
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.DataFreshness
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.workloads.RestartConfirmDialog
import name.levis.ichor.ui.workloads.RestartResultToasts
import name.levis.ichor.ui.components.TooltipIconButton

/**
 * Every app running in the cluster as a grid of icons, with search and filters; tapping one
 * opens its details. [onNode] opens a node's pods (address, hostname, role).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun AppsScreen(
    onBack: () -> Unit,
    onNode: (addr: String, host: String, role: String) -> Unit,
    vm: AppsViewModel = viewModel(factory = factory { AppsViewModel(app.talosRepository) }),
    workloadsVm: AppWorkloadsViewModel = viewModel(factory = factory { AppWorkloadsViewModel(app.talosRepository) }),
    routesVm: AppRoutesViewModel = viewModel(factory = factory { AppRoutesViewModel(app.talosRepository) }),
) {
    val application = LocalContext.current.applicationContext as TalosApp
    val state by vm.state.collectAsStateWithLifecycle()
    val config by application.configRepository.config.collectAsStateWithLifecycle()
    val invalidations by application.talosRepository.invalidations.collectAsStateWithLifecycle()
    val generation by application.configRepository.generation.collectAsStateWithLifecycle()
    LaunchedEffect(config?.activeContext, generation, invalidations) {
        vm.load(Triple(config?.activeContext, generation, invalidations))
    }
    // Hostnames and roles from the overview already on screen: no extra call.
    val nodes = remember(state) {
        application.talosRepository.cached<ClusterOverview>(OVERVIEW)?.value?.nodes.orEmpty().associateBy { it.node }
    }
    var selected by rememberSaveable { mutableStateOf<String?>(null) }
    // Rollout restarts and routes go through the Kubernetes API: only for a role that can reach it.
    val canRestart = config?.activeSummary?.allows(Feature.WORKLOADS) == true
    val workloads by workloadsVm.state.collectAsStateWithLifecycle()
    val routes by routesVm.state.collectAsStateWithLifecycle()
    val restarting by workloadsVm.restarts.restarting.collectAsStateWithLifecycle()
    var confirm by remember { mutableStateOf<KubeWorkload?>(null) }
    RestartResultToasts(workloadsVm.restarts.results)
    confirm?.let { w ->
        RestartConfirmDialog(
            workload = w,
            onConfirm = {
                confirm = null
                workloadsVm.restarts.restart(w)
            },
            onDismiss = { confirm = null },
        )
    }

    Scaffold(
        bottomBar = { DataFreshness(state) },
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text(stringResource(R.string.apps_title))
                        (state as? UiState.Loaded)?.data?.let {
                            Text(
                                screenSubtitle(it),
                                style = MaterialTheme.typography.labelMedium,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                                maxLines = 1,
                                overflow = TextOverflow.Ellipsis,
                            )
                        }
                    }
                },
                navigationIcon = { IconButton(onClick = onBack) { Icon(Icons.AutoMirrored.Filled.ArrowBack, stringResource(R.string.common_back)) } },
                actions = {
                    val busy = state == UiState.Loading || (state as? UiState.Loaded)?.refreshing == true
                    TooltipIconButton(Icons.Outlined.Refresh, stringResource(R.string.common_refresh), onClick = { vm.refresh() }, enabled = !busy)
                },
            )
        },
    ) { padding ->
        when (val s = state) {
            UiState.Loading -> LoadingBox(Modifier.padding(padding))
            is UiState.Failed -> ErrorBox(s.message, vm::refresh, Modifier.padding(padding))
            is UiState.Loaded -> PullToRefreshBox(
                isRefreshing = s.refreshing,
                onRefresh = vm::refresh,
                modifier = Modifier.padding(padding).fillMaxSize(),
            ) {
                AppsGrid(s.data, onOpen = { selected = it.id })
                s.data.apps.firstOrNull { it.id == selected }?.let { detail ->
                    if (canRestart) {
                        LaunchedEffect(detail) {
                            workloadsVm.load(detail)
                            routesVm.load(detail)
                        }
                    }
                    AppDetailSheet(
                        app = detail,
                        nodes = nodes,
                        routes = if (canRestart) routes else null,
                        restart = if (canRestart) AppRestartUi(workloads, restarting) { confirm = it } else null,
                        onPodNode = { addr -> nodes.openNode(addr, onNode) },
                        onDismiss = { selected = null },
                    )
                }
            }
        }
    }
}

private fun Map<String, NodeOverview>.openNode(addr: String, onNode: (String, String, String) -> Unit) {
    val node = this[addr]
    onNode(addr, node?.hostname ?: addr, node?.role ?: "unknown")
}

/** "12 apps · 31 containers · 5 nodes", or "… · 4 of 5 nodes" when some did not answer. */
@Composable
private fun screenSubtitle(inventory: Inventory): String {
    val containers = inventory.apps.sumOf { it.containers }
    val nodes = if (inventory.partial) {
        pluralStringResource(R.plurals.apps_nodes_partial, inventory.nodes, inventory.answered, inventory.nodes)
    } else {
        pluralStringResource(R.plurals.apps_nodes, inventory.answered, inventory.answered)
    }
    return listOf(
        pluralStringResource(R.plurals.apps_count, inventory.apps.size, inventory.apps.size),
        pluralStringResource(R.plurals.apps_containers, containers, containers),
        nodes,
    ).joinToString(" · ")
}
