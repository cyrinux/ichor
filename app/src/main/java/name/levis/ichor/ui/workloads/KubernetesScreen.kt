package name.levis.ichor.ui.workloads

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.PrimaryScrollableTabRow
import name.levis.ichor.ui.design.IchorScaffold
import name.levis.ichor.ui.components.AppTab
import androidx.compose.material3.Text
import name.levis.ichor.ui.design.IchorTextButton
import name.levis.ichor.ui.design.IchorTopAppBar
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
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.KubeRepository
import name.levis.ichor.data.isMeteredNetwork
import name.levis.ichor.data.realFingerprint
import name.levis.ichor.model.DataServiceKind
import name.levis.ichor.model.KubeFocus
import name.levis.ichor.model.KubeNamespaces
import name.levis.ichor.model.KubeScope
import name.levis.ichor.model.KubernetesAction
import name.levis.ichor.model.defaultScope
import name.levis.ichor.model.detected
import name.levis.ichor.model.ShareTarget
import name.levis.ichor.ui.share.rememberShareLink
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.argorollouts.ArgoRolloutsViewModel
import name.levis.ichor.ui.crossplane.CrossplaneViewModel
import name.levis.ichor.ui.dataservices.DataServicesViewModel
import name.levis.ichor.ui.flows.CiliumViewModel
import name.levis.ichor.ui.lifecycle.ClusterLifecycleViewModel
import name.levis.ichor.ui.tailscale.TailscaleViewModel
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.components.ActionBarActions
import name.levis.ichor.ui.components.ActionBarEditor
import name.levis.ichor.ui.components.SwipeTabPager
import name.levis.ichor.ui.components.pageContent

/** The network test's tab (its notification opens it). */
const val NETPERF_TAB = 3

/** Workloads, Pods, CronJobs and NetPerf. */
private val KUBE_TABS = listOf(0, 1, 2, NETPERF_TAB)

/**
 * The cluster's Kubernetes side, through the Kubernetes API with the admin kubeconfig Talos
 * issues (os:admin): workloads with rollout restart, pods, CronJobs with a manual run, and a
 * network test between two nodes. The namespace listed (remembered per cluster) and the
 * search carry over between the tabs. Its top bar is arranged like the overview's: the
 * actions shown as icons or kept in its menu are the user's choice. The top bar sets the API address to use instead of the
 * kubeconfig's, for a cluster the phone reaches another way (not in screenshot mode: the
 * dialog would show the real address). It also opens the API server health, the network
 * policies and, with Cilium or Calico,
 * the live flows ([onFlows] with the namespace and pod to narrow them to, or nulls), and from
 * its menu the browser of every kind ([onResources]), the Helm releases ([onHelm]) and the
 * live cluster events ([onEvents]), and the admission policies ([onPolicies]). Crossplane,
 * the Tailscale operator, Argo Rollouts and Cluster API / Sidero Metal are offered once the
 * cluster answered it runs them, as on the Kubernetes home of a cluster added from a kubeconfig.
 * [focus] (a share link) opens a tab, scoped to and searched for one item, whose sheet opens
 * once its row loads.
 */

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun KubernetesScreen(
    onBack: () -> Unit,
    onNetworkPolicies: () -> Unit,
    onApiHealth: () -> Unit,
    onCheckup: () -> Unit,
    onCapacity: () -> Unit,
    onFlows: (namespace: String?, pod: String?) -> Unit,
    onResources: () -> Unit,
    onHelm: () -> Unit,
    onStorage: () -> Unit,
    onServices: () -> Unit,
    onJobs: () -> Unit,
    onEvents: () -> Unit,
    onPolicies: () -> Unit,
    onAutoscaling: () -> Unit,
    onVirtualMachines: () -> Unit,
    onCrossplane: () -> Unit,
    onTailscale: () -> Unit,
    onArgoRollouts: () -> Unit,
    onClusterLifecycle: () -> Unit,
    /** Data services on one system (null: the hub), alone (solo) for one the Kubernetes home files elsewhere. */
    onDataServices: (DataServiceKind?, Boolean) -> Unit,
    focus: KubeFocus = KubeFocus(0),
    /** Where a pod's pull problem leads; null on a cluster without Talos nodes. */
    pullLinks: PodPullLinks? = null,
) {
    var tab by rememberSaveable { mutableIntStateOf(focus.tab) }
    var query by rememberSaveable { mutableStateOf(focus.name) }
    // The item of the link still to show; the keys of another tab are not its.
    var pending by rememberSaveable { mutableStateOf(focus.key) }
    // Not for a page only dragged into view either.
    val focusKey = { page: Int -> pending.takeIf { it.isNotEmpty() && page == focus.tab && page == tab } }
    val onFocused = { pending = "" }

    val app = LocalContext.current.applicationContext as TalosApp
    val metered = { isMeteredNetwork(app) }
    val workloads: WorkloadsViewModel = viewModel(factory = factory { WorkloadsViewModel(app.kubeRepository, app.gitOpsRepository, metered) })
    val pods: PodsViewModel = viewModel(factory = factory { PodsViewModel(app.kubeRepository, metered) })
    val cronJobs: CronJobsViewModel = viewModel(factory = factory { CronJobsViewModel(app.kubeRepository, app.gitOpsRepository, metered) })
    val namespaces: NamespacesViewModel = viewModel(factory = factory { NamespacesViewModel(app.kubeRepository) })
    val netPerf = netPerfViewModel()
    val cilium: CiliumViewModel = viewModel(factory = factory { CiliumViewModel(app.ciliumRepository) })
    val ciliumState by cilium.state.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { if (ciliumState == UiState.Loading) cilium.refresh() }
    // Unknown until read, or unreadable: no entry rather than one that fails.
    val hasFlows = (ciliumState as? UiState.Loaded)?.data?.installed == true
    // The operators' screens, offered like on the Kubernetes home: each read answers "not installed" quickly.
    val crossplane: CrossplaneViewModel = viewModel(factory = factory { CrossplaneViewModel(app.kubeBrowser) })
    val tailscale: TailscaleViewModel = viewModel(factory = factory { TailscaleViewModel(app.kubeBrowser) })
    val rollouts: ArgoRolloutsViewModel = viewModel(factory = factory { ArgoRolloutsViewModel(app.argoRolloutsRepository) })
    val lifecycle: ClusterLifecycleViewModel = viewModel(factory = factory { ClusterLifecycleViewModel(app.kubeBrowser) })
    val operators = listOf<LoadingViewModel<*>>(crossplane, tailscale, rollouts, lifecycle)
    LaunchedEffect(Unit) { operators.filter { it.state.value == UiState.Loading }.forEach { it.refresh() } }
    val crossplaneState by crossplane.state.collectAsStateWithLifecycle()
    val tailscaleState by tailscale.state.collectAsStateWithLifecycle()
    val rolloutsState by rollouts.state.collectAsStateWithLifecycle()
    val lifecycleState by lifecycle.state.collectAsStateWithLifecycle()
    val hasCrossplane = (crossplaneState as? UiState.Loaded)?.data?.installed == true
    val hasTailscale = (tailscaleState as? UiState.Loaded)?.data?.installed == true
    val hasRollouts = (rolloutsState as? UiState.Loaded)?.data?.installed == true
    val hasLifecycle = (lifecycleState as? UiState.Loaded)?.data?.installed == true
    // Which systems Data services reads, to offer those filed outside it; never blocks the tabs.
    val dataServices: DataServicesViewModel = viewModel(factory = factory { DataServicesViewModel(app.dataServicesRepository) })
    LaunchedEffect(Unit) { dataServices.load(Unit, dataServices.inventoryHints()) }
    val dataState by dataServices.state.collectAsStateWithLifecycle()
    val detected = (dataState as? UiState.Loaded)?.data?.detected.orEmpty()
    val config by app.configRepository.config.collectAsStateWithLifecycle()
    val servers by app.kubeServers.servers.collectAsStateWithLifecycle()
    val mask by app.uiPreferences.privacyMask.collectAsStateWithLifecycle()
    // The API address, set from the cluster's row in Manage clusters: a new one (also set while
    // this screen was in the back stack) reloads every list through it, and a load in flight
    // through the old one is cancelled.
    val server = config?.realFingerprint?.let { servers[it] }.orEmpty()
    var listedThrough by rememberSaveable { mutableStateOf(server) }
    LaunchedEffect(server) {
        if (server != listedThrough) {
            listedThrough = server
            (listOf<LoadingViewModel<*>>(workloads, pods, cronJobs, netPerf, cilium, namespaces, dataServices) + operators).forEach { it.refresh(reset = true) }
        }
    }
    val scope = rememberKubeScope(app, namespaces, mask.enabled, focus.namespace)
    val bar by app.uiPreferences.kubernetesBar.collectAsStateWithLifecycle()
    var customizing by rememberSaveable { mutableStateOf(false) }
    BackHandler(enabled = customizing) { customizing = false }
    val share = rememberShareLink()
    // The pods tab's "Clean up dead pods", for the namespace listed.
    var sanitizing by rememberSaveable { mutableStateOf(false) }
    if (sanitizing) {
        SanitizeSheet(scope.scope.namespace, app.kubeRepository, app.podSweepManager, config?.activeContext.orEmpty(), onDismiss = { sanitizing = false })
    }

    IchorScaffold(
        topBar = {
            if (customizing) IchorTopAppBar(
                title = { Text(stringResource(R.string.bar_edit_title)) },
                actions = { IchorTextButton(onClick = { customizing = false }) { Text(stringResource(R.string.overview_edit_done)) } },
            ) else IchorTopAppBar(
                title = { Text("Kubernetes") },
                navigationIcon = { BackButton(onBack) },
                actions = {
                    ActionBarActions(
                        bar = bar,
                        look = kubernetesActionLook,
                        onClick = { action ->
                            when (action) {
                                KubernetesAction.CHECKUP -> onCheckup()
                                KubernetesAction.NETWORK_POLICIES -> onNetworkPolicies()
                                KubernetesAction.SHARE -> share(ShareTarget.kubernetes(tab))
                                KubernetesAction.API_HEALTH -> onApiHealth()
                                KubernetesAction.FLOWS -> onFlows(scope.scope.namespace, null)
                                KubernetesAction.RESOURCES -> onResources()
                                KubernetesAction.HELM -> onHelm()
                                KubernetesAction.STORAGE -> onStorage()
                                KubernetesAction.SERVICES -> onServices()
                                KubernetesAction.JOBS -> onJobs()
                                KubernetesAction.EVENTS -> onEvents()
                                KubernetesAction.POLICIES -> onPolicies()
                                KubernetesAction.AUTOSCALING -> onAutoscaling()
                                KubernetesAction.VIRTUAL_MACHINES -> onVirtualMachines()
                                KubernetesAction.CAPACITY -> onCapacity()
                                KubernetesAction.CROSSPLANE -> onCrossplane()
                                KubernetesAction.TAILSCALE -> onTailscale()
                                KubernetesAction.ARGO_ROLLOUTS -> onArgoRollouts()
                                KubernetesAction.CLUSTER_LIFECYCLE -> onClusterLifecycle()
                                KubernetesAction.DATA_SERVICES -> onDataServices(null, false)
                                KubernetesAction.CERT_MANAGER, KubernetesAction.CASTAI ->
                                    onDataServices(action.dataServiceKind, true)
                            }
                        },
                        customizeLabel = stringResource(R.string.bar_edit_title),
                        onCustomize = { customizing = true },
                        offered = { action ->
                            when (action) {
                                KubernetesAction.SHARE -> tab < ShareTarget.KUBE_TABS.size
                                KubernetesAction.FLOWS -> hasFlows
                                KubernetesAction.CROSSPLANE -> hasCrossplane
                                KubernetesAction.TAILSCALE -> hasTailscale
                                KubernetesAction.ARGO_ROLLOUTS -> hasRollouts
                                KubernetesAction.CLUSTER_LIFECYCLE -> hasLifecycle
                                // Unknown until read, or unreadable: no entry rather than one that fails.
                                KubernetesAction.CERT_MANAGER, KubernetesAction.CASTAI ->
                                    action.dataServiceKind in detected
                                else -> true
                            }
                        },
                        groups = kubernetesActionGroups,
                    )
                },
            )
        },
    ) { padding ->
        if (customizing) {
            ActionBarEditor(bar, kubernetesActionLook, app.uiPreferences::setKubernetesBar, Modifier.pageContent(padding), groups = kubernetesActionGroups)
            return@IchorScaffold
        }
        Column(Modifier.pageContent(padding).fillMaxSize()) {
            PrimaryScrollableTabRow(selectedTabIndex = tab, edgePadding = 0.dp) {
                AppTab(selected = tab == 0, onClick = { tab = 0 }, text = { Text(stringResource(R.string.workloads_title)) })
                AppTab(selected = tab == 1, onClick = { tab = 1 }, text = { Text(stringResource(R.string.pods_title)) })
                AppTab(selected = tab == 2, onClick = { tab = 2 }, text = { Text(stringResource(R.string.cronjobs_title)) })
                AppTab(selected = tab == NETPERF_TAB, onClick = { tab = NETPERF_TAB }, text = { Text(stringResource(R.string.netperf_tab)) })
            }
            SwipeTabPager(KUBE_TABS, tab, onSelect = { tab = it }) { page ->
                when (page) {
                    0 -> WorkloadsTab(scope, query, onQuery = { query = it }, vm = workloads, focusKey = focusKey(page), onFocused = onFocused, onAutoscaling = onAutoscaling)
                    1 -> PodsTab(
                        scope,
                        query,
                        onQuery = { query = it },
                        onFlows = if (hasFlows) ({ pod -> onFlows(pod.namespace, pod.name) }) else null,
                        vm = pods,
                        focusKey = focusKey(page),
                        onFocused = onFocused,
                        pullLinks = pullLinks,
                        onCleanUp = { sanitizing = true },
                    )
                    2 -> CronJobsTab(scope, query, onQuery = { query = it }, vm = cronJobs, focusKey = focusKey(page), onFocused = onFocused)
                    else -> NetPerfTab(netPerf)
                }
            }
        }
    }
}

/** The cluster's namespaces, to pick the scope of the lists; forbidden is an answer, not a failure. */
class NamespacesViewModel(private val kube: KubeRepository) : LoadingViewModel<KubeNamespaces>() {
    override suspend fun fetch() = kube.namespaces()
}

/**
 * The scope of the Kubernetes lists (L5, L6): the one picked for the active cluster, kept on
 * the device (not in screenshot mode or the demo: then only while the screen lives), else
 * the default for what [namespaces] says. A share link's [linked] namespace comes first, not
 * remembered, until another scope is picked.
 */
@Composable
internal fun rememberKubeScope(app: TalosApp, namespaces: NamespacesViewModel, masked: Boolean, linked: String = ""): KubeScopeControl {
    val config by app.configRepository.config.collectAsStateWithLifecycle()
    val stored by app.kubeScopes.scopes.collectAsStateWithLifecycle()
    val listed by namespaces.state.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { if (listed == UiState.Loading) namespaces.refresh() }
    val cluster = config?.realFingerprint?.takeUnless { masked }
    var local by rememberSaveable { mutableStateOf<String?>(null) }
    var fromLink by rememberSaveable { mutableStateOf(linked.ifEmpty { null }) }
    val known = (listed as? UiState.Loaded)?.data
    val remembered = fromLink?.let { KubeScope(it, chosen = true) }
        ?: if (cluster != null) stored[cluster]?.let { KubeScope.fromStored(it) } else local?.let { KubeScope.fromStored(it) }
    // Null: namespaces cannot be listed and the context names none, the user types one.
    val scope = defaultScope(remembered, known)
    return remember(scope, known, cluster) {
        KubeScopeControl(scope ?: KubeScope(), known, ready = scope != null) { picked ->
            fromLink = null
            if (cluster != null) app.kubeScopes.set(cluster, picked) else local = picked.stored
        }
    }
}
