package name.levis.ichor.ui.overview

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Settings
import androidx.compose.material.icons.outlined.Widgets
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
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
import name.levis.ichor.BuildConfig
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.KUBE_NODES
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.data.activeSummary
import name.levis.ichor.model.ArgoStatus
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.FluxStatus
import name.levis.ichor.model.KubeNodeInfo
import name.levis.ichor.model.KubeNodesOverview
import name.levis.ichor.model.NodeFilter
import name.levis.ichor.monitor.freezeReminderHook
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.app
import name.levis.ichor.ui.argocd.ArgoViewModel
import name.levis.ichor.ui.components.DataFreshness
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.VersionFooter
import name.levis.ichor.ui.components.rememberClusterLabels
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.flux.FluxViewModel
import name.levis.ichor.ui.kubeauth.SignInBanner
import name.levis.ichor.ui.kubeauth.SignInSheet
import name.levis.ichor.model.KubeSignInInfo
import name.levis.ichor.model.SignInNeeded
import name.levis.ichor.model.signInNeeded
import androidx.compose.runtime.produceState
import name.levis.ichor.ui.components.pageContent

class KubeHomeViewModel(val talos: TalosRepository) : LoadingViewModel<KubeNodesOverview>() {
    override val keepsDataOnFailure = true
    override fun cached(): TalosRepository.Timed<KubeNodesOverview>? = talos.cached(KUBE_NODES)
    override val restores get() = talos.restores
    override suspend fun fetch() = talos.kubeNodes()
}

/** Where the Kubernetes home leads: Kubernetes screens only, the Talos ones cannot be read. */
class KubeHomeNavigation(
    val onWorkloads: () -> Unit,
    val onMetrics: () -> Unit,
    val onCheckup: () -> Unit,
    val onApiHealth: () -> Unit,
    val onNetworkPolicies: () -> Unit,
    val onArgoCD: () -> Unit,
    val onFlux: () -> Unit,
    val onSettings: () -> Unit,
    val onFunding: () -> Unit,
    val onAddCluster: () -> Unit,
    val onClustersCleared: () -> Unit,
    val onChangelog: () -> Unit,
    val onResources: () -> Unit,
    val onHelm: () -> Unit,
    /** The drain of a node, by its Kubernetes name. */
    val onDrain: (node: String) -> Unit,
    /** The Kubernetes nodes screen of a large cluster, on one filter (null: all). */
    val onAllNodes: (NodeFilter?) -> Unit,
)

/**
 * The home of a cluster added from a kubeconfig, in place of the Talos overview: the API
 * server and who the credentials are, the nodes as Kubernetes lists them, the Kubernetes
 * screens, and Argo CD and Flux when installed. No Talos card or action: there is no Talos API;
 * a node can still be cordoned and drained through Kubernetes.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun KubeHomeScreen(
    nav: KubeHomeNavigation,
    vm: KubeHomeViewModel = viewModel(factory = factory { KubeHomeViewModel(app.talosRepository) }),
    argoVm: ArgoViewModel = viewModel(key = "overview-argocd", factory = factory { ArgoViewModel(app.talosRepository, freezeReminderHook(app)) }),
    fluxVm: FluxViewModel = viewModel(key = "overview-flux", factory = factory { FluxViewModel(app.talosRepository) }),
) {
    val app = LocalContext.current.applicationContext as TalosApp
    val config by app.configRepository.config.collectAsStateWithLifecycle()
    val generation by app.configRepository.generation.collectAsStateWithLifecycle()
    val invalidations by vm.talos.invalidations.collectAsStateWithLifecycle()
    val state by vm.state.collectAsStateWithLifecycle()
    val clusterColors by app.clusterColors.colors.collectAsStateWithLifecycle()
    val clusterLabels = rememberClusterLabels()
    var showClusters by remember { mutableStateOf(false) }
    var clusterMenu by remember { mutableStateOf(false) }
    var signingIn by remember { mutableStateOf(false) }
    var nodeMenu by remember { mutableStateOf<KubeNodeInfo?>(null) }
    val snackbar = remember { SnackbarHostState() }
    // How the cluster signs in, read again after each sign-in (invalidations): null for static credentials.
    val signIn by produceState<KubeSignInInfo?>(null, config?.activeContext, invalidations) {
        value = config?.activeSummary?.takeIf { it.signIn.isNotEmpty() }?.let { cluster ->
            runCatching { app.kubeAuthRepository.info(cluster.name) }.getOrNull()
        }
    }

    LaunchedEffect(config?.activeContext, invalidations) { vm.refresh(reset = true) }
    // No Talos inventory hints at Argo CD or Flux here: both are asked, and answer "not
    // installed" quickly when absent.
    val gitopsKey = listOf(config?.activeContext, generation, invalidations)
    LaunchedEffect(gitopsKey) {
        argoVm.load(gitopsKey)
        fluxVm.load(gitopsKey)
    }
    val argo by argoVm.state.collectAsStateWithLifecycle()
    val flux by fluxVm.state.collectAsStateWithLifecycle()

    Scaffold(
        snackbarHost = { SnackbarHost(snackbar) },
        bottomBar = { DataFreshness(state) },
        topBar = {
            TopAppBar(
                modifier = Modifier.clusterSwipe(config, app::selectCluster),
                title = {
                    Box {
                        ClusterTitle(config, clusterColors, clusterLabels, onOpen = { clusterMenu = true }) { ScreenshotModeIcon() }
                        config?.let { stored ->
                            ClusterMenu(
                                expanded = clusterMenu,
                                config = stored,
                                colors = clusterColors,
                                labels = clusterLabels,
                                onSelect = app::selectCluster,
                                onManage = { showClusters = true },
                                onDismiss = { clusterMenu = false },
                            )
                        }
                    }
                },
                actions = {
                    TooltipIconButton(Icons.Outlined.Widgets, stringResource(R.string.overview_action_workloads), onClick = nav.onWorkloads)
                    TooltipIconButton(Icons.Outlined.Settings, stringResource(R.string.overview_action_settings), onClick = nav.onSettings)
                },
            )
        },
    ) { padding ->
        config?.takeIf { showClusters }?.let { stored ->
            ManageClustersSheet(
                config = stored,
                colors = clusterColors,
                labels = clusterLabels,
                onClose = { showClusters = false },
                onAddCluster = nav.onAddCluster,
                onClustersCleared = nav.onClustersCleared,
            )
        }
        val refresh = {
            vm.refresh()
            argoVm.refresh()
            fluxVm.refresh()
        }
        if (signingIn) {
            config?.activeContext?.let { name ->
                SignInSheet(context = name, onDismiss = { signingIn = false }, onSignedIn = { signingIn = false })
            }
        }
        when (val s = state) {
            UiState.Loading -> LoadingBox(Modifier.pageContent(padding))
            else -> PullToRefreshBox(
                isRefreshing = (s as? UiState.Loaded)?.refreshing == true,
                onRefresh = refresh,
                modifier = Modifier.pageContent(padding).fillMaxSize(),
            ) {
                KubeHomeList(
                    cluster = config?.activeSummary,
                    name = config?.activeSummary?.let(clusterLabels::of),
                    nodes = (s as? UiState.Loaded)?.data,
                    failure = (s as? UiState.Failed)?.message,
                    signIn = kubeSignInBanner(signIn, (s as? UiState.Failed)?.message ?: (s as? UiState.Loaded)?.error),
                    onSignIn = { signingIn = true },
                    onRetry = vm::refresh,
                    argo = argo.takeIf { (it as? UiState.Loaded)?.data?.installed == true },
                    flux = flux.takeIf { (it as? UiState.Loaded)?.data?.installed == true },
                    onNode = { nodeMenu = it },
                    nav = nav,
                )
            }
        }
    }
    KubeNodeActionSheets(nodeMenu, snackbar, onClose = { nodeMenu = null }, onDrain = nav.onDrain, onCordoned = vm::refresh)
}

@Composable
private fun KubeHomeList(
    cluster: ContextSummary?,
    name: String?,
    nodes: KubeNodesOverview?,
    failure: UiText?,
    signIn: KubeSignInBanner?,
    onSignIn: () -> Unit,
    onRetry: () -> Unit,
    argo: UiState<ArgoStatus>?,
    flux: UiState<FluxStatus>?,
    onNode: (KubeNodeInfo) -> Unit,
    nav: KubeHomeNavigation,
) {
    LazyColumn(
        contentPadding = PaddingValues(16.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
        modifier = Modifier.fillMaxSize(),
    ) {
        if (BuildConfig.SELF_UPDATE) item { UpdateBanner(onClick = nav.onSettings) } else item { StoreUpdateBanner() }
        if (BuildConfig.DONATIONS || BuildConfig.FEATURE_FUNDING) item { SupportCard(nav.onFunding) }
        cluster?.let { item { KubeCredentialsBanner(it) } }
        signIn?.let { item(key = "sign-in") { SignInBanner(it.method, it.needed, onSignIn) } }
        // The banner says it all when the call failed for want of a sign-in.
        failure?.takeIf { signIn?.needed == null }?.let { item(key = "failure") { KubeUnreachableCard(it, onRetry) } }
        cluster?.let { item(key = "summary") { KubeSummaryCard(name ?: it.name, it, nodes) } }
        if (nodes != null) item(key = "nodes") { KubeNodesCard(nodes, onNode, nav.onAllNodes) }
        item(key = "tools") { KubeToolsCard(nav) }
        argo?.let { item(key = "argocd") { ArgoCard(it, argoTile = null, downNodes = emptySet(), onOpen = nav.onArgoCD) } }
        flux?.let { item(key = "flux") { FluxCard(it, fluxTile = null, onOpen = nav.onFlux) } }
        item(key = "version") { VersionFooter(onClick = nav.onChangelog) }
    }
}

/** The sign-in banner of the kube home: the [method], and what a failed call said ([needed]). */
internal data class KubeSignInBanner(val method: String, val needed: SignInNeeded?)

/**
 * The banner to show, if any: the cluster was never signed in ([info]), or a call failed
 * asking for a sign-in ([failure], the core's message).
 */
internal fun kubeSignInBanner(info: KubeSignInInfo?, failure: UiText?): KubeSignInBanner? {
    val needed = when (failure) {
        is UiText.SignInRequired -> SignInNeeded(failure.method, failure.reason)
        is UiText.Raw -> signInNeeded(failure.text)
        else -> null
    }
    return when {
        needed != null -> KubeSignInBanner(info?.method ?: needed.method, needed)
        info != null && !info.signedIn -> KubeSignInBanner(info.method, null)
        else -> null
    }
}
