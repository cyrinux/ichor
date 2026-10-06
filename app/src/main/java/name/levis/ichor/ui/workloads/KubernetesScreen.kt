package name.levis.ichor.ui.workloads

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Dns
import androidx.compose.material.icons.outlined.MonitorHeart
import androidx.compose.material.icons.outlined.HealthAndSafety
import androidx.compose.material.icons.outlined.Policy
import androidx.compose.material.icons.outlined.Stream
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.PrimaryScrollableTabRow
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Tab
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
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
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.data.isMeteredNetwork
import name.levis.ichor.data.realFingerprint
import name.levis.ichor.model.KubeFocus
import name.levis.ichor.model.KubeNamespaces
import name.levis.ichor.model.KubeScope
import name.levis.ichor.model.defaultScope
import name.levis.ichor.model.ShareTarget
import name.levis.ichor.ui.share.ShareLinkButton
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.flows.CiliumViewModel
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.SwipeTabPager

/** Workloads, Pods, CronJobs and NetPerf. */
private val KUBE_TABS = listOf(0, 1, 2, 3)

/**
 * The cluster's Kubernetes side, through the Kubernetes API with the admin kubeconfig Talos
 * issues (os:admin): workloads with rollout restart, pods, CronJobs with a manual run, and a
 * network test between two nodes. The namespace listed (remembered per cluster) and the
 * search carry over between the tabs. The top bar sets the API address to use instead of the
 * kubeconfig's, for a cluster the phone reaches another way (not in screenshot mode: the
 * dialog would show the real address). It also opens the API server health, the network
 * policies and, with Cilium,
 * the live flows ([onFlows] with the namespace and pod to narrow them to, or nulls).
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
    onFlows: (namespace: String?, pod: String?) -> Unit,
    focus: KubeFocus = KubeFocus(0),
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
    val workloads: WorkloadsViewModel = viewModel(factory = factory { WorkloadsViewModel(app.talosRepository, metered) })
    val pods: PodsViewModel = viewModel(factory = factory { PodsViewModel(app.talosRepository, metered) })
    val cronJobs: CronJobsViewModel = viewModel(factory = factory { CronJobsViewModel(app.talosRepository, metered) })
    val namespaces: NamespacesViewModel = viewModel(factory = factory { NamespacesViewModel(app.talosRepository) })
    val netPerf = netPerfViewModel()
    val cilium: CiliumViewModel = viewModel(factory = factory { CiliumViewModel(app.ciliumRepository) })
    val ciliumState by cilium.state.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { if (ciliumState == UiState.Loading) cilium.refresh() }
    // Unknown until read, or unreadable: no entry rather than one that fails.
    val hasCilium = (ciliumState as? UiState.Loaded)?.data?.installed == true
    val config by app.configRepository.config.collectAsStateWithLifecycle()
    val servers by app.kubeServers.servers.collectAsStateWithLifecycle()
    val mask by app.uiPreferences.privacyMask.collectAsStateWithLifecycle()
    val fingerprint = config?.realFingerprint?.takeUnless { mask.enabled }
    var editing by remember { mutableStateOf(false) }
    // Screenshot mode turned on with the dialog open: closed, not just hidden until it is off.
    LaunchedEffect(fingerprint) { if (fingerprint == null) editing = false }
    val scope = rememberKubeScope(app, namespaces, mask.enabled, focus.namespace)

    if (editing && fingerprint != null) {
        KubeServerDialog(
            saved = servers[fingerprint].orEmpty(),
            onSave = { server ->
                editing = false
                if (server != servers[fingerprint].orEmpty()) {
                    app.setKubeServer(fingerprint, server)
                    // Both reload through the new address; a load in flight through the old one is cancelled.
                    listOf<LoadingViewModel<*>>(workloads, pods, cronJobs, netPerf, cilium, namespaces).forEach { it.refresh(reset = true) }
                }
            },
            onDismiss = { editing = false },
        )
    }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text("Kubernetes") },
                navigationIcon = { BackButton(onBack) },
                actions = {
                    if (tab < ShareTarget.KUBE_TABS.size) ShareLinkButton(ShareTarget.kubernetes(tab))
                    TooltipIconButton(Icons.Outlined.HealthAndSafety, stringResource(R.string.checkup_title), onClick = onCheckup)
                    TooltipIconButton(Icons.Outlined.MonitorHeart, stringResource(R.string.apihealth_title), onClick = onApiHealth)
                    TooltipIconButton(Icons.Outlined.Policy, stringResource(R.string.netpol_title), onClick = onNetworkPolicies)
                    if (hasCilium) {
                        TooltipIconButton(Icons.Outlined.Stream, stringResource(R.string.flows_title), onClick = { onFlows(scope.scope.namespace, null) })
                    }
                    if (fingerprint != null) {
                        TooltipIconButton(Icons.Outlined.Dns, stringResource(R.string.kube_server_title), onClick = { editing = true })
                    }
                },
            )
        },
    ) { padding ->
        Column(Modifier.padding(padding).fillMaxSize()) {
            PrimaryScrollableTabRow(selectedTabIndex = tab, edgePadding = 0.dp) {
                Tab(selected = tab == 0, onClick = { tab = 0 }, text = { Text(stringResource(R.string.workloads_title)) })
                Tab(selected = tab == 1, onClick = { tab = 1 }, text = { Text(stringResource(R.string.pods_title)) })
                Tab(selected = tab == 2, onClick = { tab = 2 }, text = { Text(stringResource(R.string.cronjobs_title)) })
                Tab(selected = tab == 3, onClick = { tab = 3 }, text = { Text(stringResource(R.string.netperf_tab)) })
            }
            SwipeTabPager(KUBE_TABS, tab, onSelect = { tab = it }) { page ->
                when (page) {
                    0 -> WorkloadsTab(scope, query, onQuery = { query = it }, vm = workloads, focusKey = focusKey(page), onFocused = onFocused)
                    1 -> PodsTab(
                        scope,
                        query,
                        onQuery = { query = it },
                        onFlows = if (hasCilium) ({ pod -> onFlows(pod.namespace, pod.name) }) else null,
                        vm = pods,
                        focusKey = focusKey(page),
                        onFocused = onFocused,
                    )
                    2 -> CronJobsTab(scope, query, onQuery = { query = it }, vm = cronJobs, focusKey = focusKey(page), onFocused = onFocused)
                    else -> NetPerfTab(netPerf)
                }
            }
        }
    }
}

/** The cluster's namespaces, to pick the scope of the lists; forbidden is an answer, not a failure. */
class NamespacesViewModel(private val talos: TalosRepository) : LoadingViewModel<KubeNamespaces>() {
    override suspend fun fetch() = talos.namespaces()
}

/**
 * The scope of the Kubernetes lists (L5, L6): the one picked for the active cluster, kept on
 * the device (not in screenshot mode or the demo: then only while the screen lives), else
 * the default for what [namespaces] says. A share link's [linked] namespace comes first, not
 * remembered, until another scope is picked.
 */
@Composable
private fun rememberKubeScope(app: TalosApp, namespaces: NamespacesViewModel, masked: Boolean, linked: String = ""): KubeScopeControl {
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
