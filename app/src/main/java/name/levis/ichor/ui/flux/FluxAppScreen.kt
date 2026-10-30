package name.levis.ichor.ui.flux

import androidx.compose.runtime.setValue
import androidx.compose.runtime.getValue
import name.levis.ichor.ui.components.ClusterScreenTitle
import name.levis.ichor.model.ShareTarget
import name.levis.ichor.ui.components.Loaded
import name.levis.ichor.ui.share.PinButton
import name.levis.ichor.ui.share.AddToHomeButton
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
import name.levis.ichor.ui.design.IchorScaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
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
import name.levis.ichor.model.FluxAction
import name.levis.ichor.model.FluxDependencyTree
import name.levis.ichor.model.dependencyTree
import name.levis.ichor.model.FluxResource
import name.levis.ichor.model.KubeWorkload
import name.levis.ichor.model.NodeOverview
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.argocd.UnhealthyPodRow
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.dataservices.downHostnames
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.gitops.GitOpsSourceSection
import name.levis.ichor.ui.gitops.ProposalsCard
import name.levis.ichor.ui.gitops.SOURCE_TOOL_FLUX
import name.levis.ichor.ui.workloads.KubeEventsList
import name.levis.ichor.ui.workloads.RestartConfirmDialog
import name.levis.ichor.ui.workloads.RestartResultToasts
import name.levis.ichor.ui.workloads.RolloutStatusSheet
import name.levis.ichor.ui.workloads.rememberRolloutFollow
import name.levis.ichor.ui.components.pageContent

/**
 * One Kustomization or HelmRelease: the hero, its actions (each confirmed), its conditions, the
 * pods that are not ready (opening their node's pods through [onNode], with the tab), a
 * Kustomization's inventory by kind (rollout restarts) and a release's history. Polls while
 * something reconciles. [onDiff] opens a Kustomization's diff. [initialReconcile]: its reconcile
 * confirmation opens once the object is read (an alert's Reconcile button).
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
    /** Opens another Kustomization or HelmRelease (a link of the dependsOn tree). */
    onApp: ((FluxApp) -> Unit)? = null,
    initialReconcile: Boolean = false,
    /** Opens the source browser on the app's source at that index. */
    onSource: ((Int) -> Unit)? = null,
) {
    val talos = LocalContext.current.applicationContext as TalosApp
    val vm: FluxViewModel = viewModel(factory = factory { FluxViewModel(talos.gitOpsRepository, talos.kubeRepository) })
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

    IchorScaffold(
        snackbarHost = { SnackbarHost(snackbar) },
        topBar = {
            TopAppBar(
                title = { ClusterScreenTitle(name) },
                navigationIcon = { BackButton(onBack) },
                actions = {
                    PinButton(ShareTarget.fluxApp(kind, namespace, name), name)
                    ShareLinkButton(ShareTarget.fluxApp(kind, namespace, name))
                    AddToHomeButton(ShareTarget.fluxApp(kind, namespace, name), name)
                    TooltipIconButton(Icons.Outlined.Refresh, stringResource(R.string.common_refresh), onClick = { vm.refresh() })
                },
            )
        },
    ) { padding ->
        val modifier = Modifier.pageContent(padding)
        Loaded(state, vm::refresh, modifier, freshness = true) { data ->
            if (app == null) {
                EmptyText(stringResource(R.string.flux_app_gone, name))
            } else {
                val overview = remember(data) { talos.talosRepository.cached<ClusterOverview>(OVERVIEW)?.value }
                val tree = remember(data, app) { data.dependencyTree(app) }
                AppDetail(app, tree, app.key in busy, vm, overview, onNode, onDiff, onApp, initialReconcile, onSource)
            }
        }
    }
}

@Composable
private fun AppDetail(
    app: FluxApp,
    tree: FluxDependencyTree,
    busy: Boolean,
    vm: FluxViewModel,
    overview: ClusterOverview?,
    onNode: ((NodeOverview, Int) -> Unit)?,
    onDiff: (() -> Unit)?,
    onApp: ((FluxApp) -> Unit)?,
    initialReconcile: Boolean,
    onSource: ((Int) -> Unit)?,
) {
    var confirm by remember { mutableStateOf<FluxConfirm?>(null) }
    // Asked from an alert: the confirmation once (not again on rotation).
    var initialReconcileShown by rememberSaveable { mutableStateOf(false) }
    if (!initialReconcileShown && initialReconcile) {
        initialReconcileShown = true
        confirm = FluxConfirm(FluxAction.RECONCILE, app.kind, app.namespace, app.name, owner = app.ownerKey)
    }
    var restart by remember { mutableStateOf<KubeWorkload?>(null) }
    val talos = LocalContext.current.applicationContext as TalosApp
    val followRollout = rememberRolloutFollow()
    val downNodes = remember(overview) { overview?.downHostnames().orEmpty() }
    val nodesByName = remember(overview) { overview?.nodes.orEmpty().associateBy { it.hostname } }

    confirm?.let { c ->
        FluxConfirmDialog(
            c,
            onConfirm = {
                confirm = null
                // A reconcile is followed app-wide once requested: its notification says when it is Ready.
                vm.act(c.kind, c.namespace, c.name, c.action) {
                    if (c.action in FOLLOWED_ACTIONS) runCatching { talos.gitOpsOperations.followFluxReconcile(c.kind, c.namespace, c.name) }
                }
            },
            onDismiss = { confirm = null },
        )
    }
    restart?.let { w ->
        // The replicas decide the downtime warning: read them fresh, unless dismissed meanwhile.
        LaunchedEffect(w.key) {
            val fresh = vm.restarts.current(w)
            if (restart?.key == w.key) restart = fresh
        }
        RestartConfirmDialog(w, onConfirm = { restart = null; vm.restarts.restart(w, followRollout) }, onDismiss = { restart = null })
    }

    LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(16.dp)) {
        item(key = "hero") { FluxAppHero(app) }
        item(key = "actions") {
            FluxActionButtons(
                app,
                busy,
                onDiff,
                onCorrectDrift = {
                    confirm = FluxConfirm(FluxAction.FORCE, app.kind, app.namespace, app.name, owner = app.ownerKey, correctDrift = true)
                },
            ) { action ->
                confirm = FluxConfirm(action, app.kind, app.namespace, app.name, owner = app.ownerKey)
            }
        }
        item(key = "source") {
            GitOpsSourceSection(SOURCE_TOOL_FLUX, app.kind, app.namespace, app.name, app.revision, onSource)
        }
        // The pull requests opened from the phone for it; a merged one not running yet reconciles with its source from here.
        item(key = "proposals") {
            ProposalsCard(talos.gitOpsRepository, SOURCE_TOOL_FLUX, app.kind, app.namespace, app.name, refresh = app.revision) {
                confirm = FluxConfirm(FluxAction.RECONCILE_WITH_SOURCE, app.kind, app.namespace, app.name, owner = app.ownerKey)
            }
        }
        // What it waits for comes before its conditions: the link that is not Ready explains them.
        if (!tree.isEmpty) {
            item(key = "depends") { FluxDependsOnTree(tree) { other -> onApp?.invoke(other) } }
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
        item(key = "events-title") { SectionTitle(stringResource(R.string.kube_events_title)) }
        item(key = "events") { KubeEventsList(app.namespace, app.kind, app.name) }
    }
}

/** The Kustomization that applies [this] from Git, "namespace/name"; empty for none. */
private val FluxApp.ownerKey: String get() = owner?.let { "${it.namespace}/${it.name}" }.orEmpty()

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

/** The actions whose reconcile is followed app-wide. */
private val FOLLOWED_ACTIONS = setOf(FluxAction.RECONCILE, FluxAction.RECONCILE_WITH_SOURCE)
