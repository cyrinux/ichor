package name.levis.ichor.ui.flux

import name.levis.ichor.model.ShareTarget
import name.levis.ichor.ui.share.ShareLinkButton
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.OVERVIEW
import name.levis.ichor.model.ClusterOverview
import name.levis.ichor.model.FluxApp
import name.levis.ichor.model.FluxResource
import name.levis.ichor.model.KubeWorkload
import name.levis.ichor.model.NodeOverview
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.argocd.UnhealthyPodRow
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.DataFreshness
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.dataservices.downHostnames
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.workloads.RestartConfirmDialog
import name.levis.ichor.ui.workloads.RestartResultToasts
import name.levis.ichor.ui.workloads.RolloutStatusSheet

/**
 * One Kustomization or HelmRelease: the hero, its actions (each confirmed), its conditions, the
 * pods that are not ready (opening their node's pods through [onNode], with the tab), a
 * Kustomization's inventory by kind (rollout restarts) and a release's history. Polls while
 * something reconciles. [onDiff] opens a Kustomization's diff.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun FluxAppScreen(
    kind: String,
    namespace: String,
    name: String,
    onBack: () -> Unit,
    onNode: ((NodeOverview, Int) -> Unit)? = null,
    onDiff: (() -> Unit)? = null,
) {
    val talos = LocalContext.current.applicationContext as TalosApp
    val vm: FluxViewModel = viewModel(factory = factory { FluxViewModel(talos.talosRepository) })
    val state by vm.state.collectAsStateWithLifecycle()
    val busy by vm.busy.collectAsStateWithLifecycle()
    val config by talos.configRepository.config.collectAsStateWithLifecycle()
    val generation by talos.configRepository.generation.collectAsStateWithLifecycle()
    val invalidations by talos.talosRepository.invalidations.collectAsStateWithLifecycle()
    LaunchedEffect(config?.activeContext, generation, invalidations) { vm.load(Triple(config?.activeContext, generation, invalidations)) }
    FluxPolling(vm)
    val snackbar = remember { SnackbarHostState() }
    FluxActionMessages(vm.results) { snackbar.showSnackbar(it) }
    RestartResultToasts(vm.restarts.results)
    RolloutStatusSheet(vm.restarts)
    val app = (state as? UiState.Loaded)?.data?.apps?.firstOrNull { it.kind == kind && it.namespace == namespace && it.name == name }

    Scaffold(
        snackbarHost = { SnackbarHost(snackbar) },
        topBar = {
            TopAppBar(
                title = { Text(name) },
                navigationIcon = { BackButton(onBack) },
                actions = {
                    ShareLinkButton(ShareTarget.fluxApp(kind, namespace, name))
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
                        EmptyText(stringResource(R.string.flux_app_gone, name))
                    } else {
                        val overview = remember(s.data) { talos.talosRepository.cached<ClusterOverview>(OVERVIEW)?.value }
                        AppDetail(app, app.key in busy, vm, overview, onNode, onDiff)
                    }
                }
                DataFreshness(s, edgeToEdge = false)
            }
        }
    }
}

@Composable
private fun AppDetail(app: FluxApp, busy: Boolean, vm: FluxViewModel, overview: ClusterOverview?, onNode: ((NodeOverview, Int) -> Unit)?, onDiff: (() -> Unit)?) {
    var confirm by remember { mutableStateOf<FluxConfirm?>(null) }
    var restart by remember { mutableStateOf<KubeWorkload?>(null) }
    val downNodes = remember(overview) { overview?.downHostnames().orEmpty() }
    val nodesByName = remember(overview) { overview?.nodes.orEmpty().associateBy { it.hostname } }

    confirm?.let { c ->
        FluxConfirmDialog(c, onConfirm = { confirm = null; vm.act(c.kind, c.namespace, c.name, c.action) }, onDismiss = { confirm = null })
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
        item(key = "hero") { FluxAppHero(app) }
        item(key = "actions") {
            FluxActionButtons(app, busy, onDiff) { action ->
                confirm = FluxConfirm(action, app.kind, app.namespace, app.name, owner = app.owner?.let { "${it.namespace}/${it.name}" }.orEmpty())
            }
        }
        if (app.conditions.isNotEmpty()) {
            item(key = "conditions-title") { SectionTitle(stringResource(R.string.flux_conditions)) }
            item(key = "conditions") { FluxConditions(app.conditions) }
        }
        if (app.unhealthyPods.isNotEmpty()) {
            item(key = "pods-title") { SectionTitle(stringResource(R.string.argo_unhealthy_pods)) }
            items(app.unhealthyPods, key = { "pod/${it.key}" }) { pod ->
                // The pod's node, on its Pods tab, when Talos knows it.
                val node = nodesByName[pod.node]?.takeIf { onNode != null }
                Box(if (node != null) Modifier.clickable(onClickLabel = stringResource(R.string.argo_net_open_node)) { onNode?.invoke(node, PODS_TAB) } else Modifier) {
                    UnhealthyPodRow(pod, pod.node.isNotEmpty() && pod.node in downNodes)
                }
            }
        }
        resources(app) { restart = vm.workloadFor(it) }
        history(app)
    }
}

/** The node screen's Pods tab. */
private const val PODS_TAB = 4

private fun LazyListScope.resources(app: FluxApp, onRestart: (FluxResource) -> Unit) {
    if (app.resources.isEmpty()) return
    item(key = "resources-title") { SectionTitle(stringResource(R.string.flux_inventory, app.resources.size)) }
    app.resourcesByKind.forEach { (kind, list) ->
        item(key = "resources/$kind") { FluxResourceGroup(kind, list, onRestart) }
    }
}

private fun LazyListScope.history(app: FluxApp) {
    if (app.history.isEmpty()) return
    item(key = "history-title") { SectionTitle(stringResource(R.string.argo_history)) }
    item(key = "history") {
        Column {
            app.history.forEachIndexed { i, h -> FluxHistoryRow(h, current = i == 0, last = i == app.history.lastIndex) }
        }
    }
}
