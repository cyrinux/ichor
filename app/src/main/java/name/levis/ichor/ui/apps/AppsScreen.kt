package name.levis.ichor.ui.apps

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
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
import name.levis.ichor.monitor.freezeReminderHook
import name.levis.ichor.TalosApp
import name.levis.ichor.data.OVERVIEW
import name.levis.ichor.data.activeSummary
import name.levis.ichor.model.ClusterOverview
import name.levis.ichor.model.Feature
import name.levis.ichor.model.allows
import name.levis.ichor.model.Inventory
import name.levis.ichor.model.InventoryApp
import name.levis.ichor.model.ARGO_CD_CATALOG_ID
import name.levis.ichor.model.ArgoAction
import name.levis.ichor.model.ArgoStatus
import name.levis.ichor.model.argoAppsFor
import name.levis.ichor.model.hasArgoCD
import name.levis.ichor.model.FLUX_CATALOG_ID
import name.levis.ichor.model.hasFlux
import name.levis.ichor.ui.components.Loaded
import name.levis.ichor.ui.flux.FluxViewModel
import name.levis.ichor.model.inventoryBadges
import name.levis.ichor.data.ARGO_CD
import name.levis.ichor.ui.argocd.ArgoActionToasts
import name.levis.ichor.ui.argocd.ArgoPolling
import name.levis.ichor.ui.argocd.ArgoViewModel
import name.levis.ichor.model.KubeWorkload
import name.levis.ichor.model.PodSelection
import name.levis.ichor.model.podSelection
import name.levis.ichor.model.NodeOverview
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.DataFreshness
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.workloads.RestartConfirmDialog
import name.levis.ichor.ui.workloads.RestartResultToasts
import name.levis.ichor.ui.workloads.RolloutStatusSheet
import name.levis.ichor.ui.workloads.WorkloadPodsSheet
import name.levis.ichor.ui.components.TooltipIconButton

/**
 * Every app running in the cluster as a grid of icons, with search and filters; tapping one
 * opens its details. [onNode] opens a node's pods (address, hostname, role); [onArgoCD] the Argo
 * CD screen and [onArgoApp] one of its apps (namespace, name); [onFlux] the Flux screen.
 * [attention] opens it on the "needs attention" chip.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun AppsScreen(
    onBack: () -> Unit,
    attention: Boolean = false,
    onNode: (addr: String, host: String, role: String) -> Unit,
    onArgoCD: () -> Unit,
    onArgoApp: (namespace: String, name: String) -> Unit,
    onFlux: () -> Unit,
    vm: AppsViewModel = viewModel(factory = factory { AppsViewModel(app.talosRepository) }),
    workloadsVm: AppWorkloadsViewModel = viewModel(factory = factory { AppWorkloadsViewModel(app.talosRepository) }),
    routesVm: AppRoutesViewModel = viewModel(factory = factory { AppRoutesViewModel(app.talosRepository) }),
    argoVm: ArgoViewModel = viewModel(key = "apps-argocd", factory = factory { ArgoViewModel(app.talosRepository, freezeReminderHook(app)) }),
    fluxVm: FluxViewModel = viewModel(key = "apps-flux", factory = factory { FluxViewModel(app.talosRepository) }),
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
    // A workload's pods, over the app's sheet.
    var podsOf by remember { mutableStateOf<PodSelection.OfWorkload?>(null) }
    RestartResultToasts(workloadsVm.restarts.results)
    RolloutStatusSheet(workloadsVm.restarts)
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

    // Argo CD: through the Kubernetes API too, and only when the inventory shows it.
    val inventory = (state as? UiState.Loaded)?.data
    val argoOffered = canRestart && inventory?.hasArgoCD == true
    val argoState by argoVm.state.collectAsStateWithLifecycle()
    val argoBusy by argoVm.busy.collectAsStateWithLifecycle()
    ArgoActionToasts(argoVm.results)
    ArgoPolling(argoVm)
    // Tile badges only from a result already at hand: the grid never asks Argo CD itself.
    val argoBadges = remember(argoState, inventory, argoOffered) {
        val status = (argoState as? UiState.Loaded)?.data ?: application.talosRepository.cached<ArgoStatus>(ARGO_CD)?.value
        if (argoOffered && status != null && inventory != null) status.inventoryBadges(inventory.apps) else emptyMap()
    }

    // Flux: its own tile's sheet only, when the inventory shows it.
    val fluxOffered = canRestart && inventory?.hasFlux == true
    val fluxState by fluxVm.state.collectAsStateWithLifecycle()

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
                navigationIcon = { BackButton(onBack) },
                actions = {
                    val busy = state == UiState.Loading || (state as? UiState.Loaded)?.refreshing == true
                    TooltipIconButton(Icons.Outlined.Refresh, stringResource(R.string.common_refresh), onClick = { vm.refresh() }, enabled = !busy)
                },
            )
        },
    ) { padding ->
        Loaded(state, vm::refresh, Modifier.padding(padding)) { data ->
            AppsGrid(data, onOpen = { selected = it.id }, argoBadges = argoBadges, attention = attention)
            data.apps.firstOrNull { it.id == selected }?.let { detail ->
                if (canRestart) {
                    LaunchedEffect(detail) {
                        workloadsVm.load(detail)
                        routesVm.load(detail)
                    }
                }
                if (argoOffered) {
                    LaunchedEffect(Unit) { argoVm.loadOrReuse(Triple(config?.activeContext, generation, invalidations)) }
                }
                val isFlux = fluxOffered && detail.id == FLUX_CATALOG_ID
                if (isFlux) {
                    LaunchedEffect(Unit) { fluxVm.loadOrReuse(Triple(config?.activeContext, generation, invalidations)) }
                }
                AppDetailSheet(
                    app = detail,
                    nodes = nodes,
                    routes = if (canRestart) routes else null,
                    restart = if (canRestart) {
                        AppRestartUi(workloads, restarting, onRestart = { confirm = it }, onPods = { podsOf = it.podSelection })
                    } else {
                        null
                    },
                    argo = if (argoOffered) argoUi(detail, argoState, argoBusy, argoVm, onArgoCD, onArgoApp) else null,
                    flux = if (isFlux) AppFluxUi(fluxState, onFlux) else null,
                    onPodNode = { addr -> nodes.openNode(addr, onNode) },
                    onDismiss = { selected = null },
                )
                podsOf?.let { WorkloadPodsSheet(it, onDismiss = { podsOf = null }) }
            }
            
        }
    }
}

/** The Argo CD section of [detail]'s sheet. */
private fun argoUi(
    detail: InventoryApp,
    state: UiState<ArgoStatus>,
    busy: Set<String>,
    vm: ArgoViewModel,
    onArgoCD: () -> Unit,
    onArgoApp: (String, String) -> Unit,
) = AppArgoUi(
    state = state,
    apps = (state as? UiState.Loaded)?.data?.let { argoAppsFor(detail, it) }.orEmpty(),
    isArgoCD = detail.id == ARGO_CD_CATALOG_ID,
    busy = busy,
    onSync = { vm.act(listOf(it), ArgoAction.SYNC) },
    onRefresh = { vm.act(listOf(it), ArgoAction.REFRESH) },
    onOpen = { onArgoApp(it.namespace, it.name) },
    onOpenArgoCD = onArgoCD,
)

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
