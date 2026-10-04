package name.levis.ichor.ui.workloads

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Dns
import androidx.compose.material.icons.outlined.Policy
import androidx.compose.material.icons.outlined.Stream
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.PrimaryTabRow
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
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.activeSummary
import name.levis.ichor.model.isDemo
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.flows.CiliumViewModel
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.components.TooltipIconButton

/**
 * The cluster's Kubernetes side, through the Kubernetes API with the admin kubeconfig Talos
 * issues (os:admin): workloads with rollout restart, pods, CronJobs with a manual run, and a
 * network test between two nodes. The namespace filter and the
 * search carry over between the tabs. The top bar sets the API address to use instead of the
 * kubeconfig's, for a cluster the phone reaches another way (not in screenshot mode: the
 * dialog would show the real address). It also opens the network policies and, with Cilium,
 * the live flows ([onFlows] with the namespace and pod to narrow them to, or nulls).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun KubernetesScreen(onBack: () -> Unit, onNetworkPolicies: () -> Unit, onFlows: (namespace: String?, pod: String?) -> Unit) {
    var tab by rememberSaveable { mutableIntStateOf(0) }
    var namespace by rememberSaveable { mutableStateOf<String?>(null) }
    var query by rememberSaveable { mutableStateOf("") }

    val app = LocalContext.current.applicationContext as TalosApp
    val workloads: WorkloadsViewModel = viewModel(factory = factory { WorkloadsViewModel(app.talosRepository) })
    val pods: PodsViewModel = viewModel(factory = factory { PodsViewModel(app.talosRepository) })
    val cronJobs: CronJobsViewModel = viewModel(factory = factory { CronJobsViewModel(app.talosRepository) })
    val netPerf = netPerfViewModel()
    val cilium: CiliumViewModel = viewModel(factory = factory { CiliumViewModel(app.ciliumRepository) })
    val ciliumState by cilium.state.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { if (ciliumState == UiState.Loading) cilium.refresh() }
    // Unknown until read, or unreadable: no entry rather than one that fails.
    val hasCilium = (ciliumState as? UiState.Loaded)?.data?.installed == true
    val config by app.configRepository.config.collectAsStateWithLifecycle()
    val servers by app.kubeServers.servers.collectAsStateWithLifecycle()
    val mask by app.uiPreferences.privacyMask.collectAsStateWithLifecycle()
    val fingerprint = config?.activeSummary?.takeIf { !it.isDemo && !mask.enabled }?.fingerprint?.takeIf { it.isNotBlank() }
    var editing by remember { mutableStateOf(false) }
    // Screenshot mode turned on with the dialog open: closed, not just hidden until it is off.
    LaunchedEffect(fingerprint) { if (fingerprint == null) editing = false }

    if (editing && fingerprint != null) {
        KubeServerDialog(
            saved = servers[fingerprint].orEmpty(),
            onSave = { server ->
                editing = false
                if (server != servers[fingerprint].orEmpty()) {
                    app.setKubeServer(fingerprint, server)
                    // Both reload through the new address; a load in flight through the old one is cancelled.
                    listOf<LoadingViewModel<*>>(workloads, pods, cronJobs, netPerf, cilium).forEach { it.refresh(reset = true) }
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
                    TooltipIconButton(Icons.Outlined.Policy, stringResource(R.string.netpol_title), onClick = onNetworkPolicies)
                    if (hasCilium) {
                        TooltipIconButton(Icons.Outlined.Stream, stringResource(R.string.flows_title), onClick = { onFlows(namespace, null) })
                    }
                    if (fingerprint != null) {
                        TooltipIconButton(Icons.Outlined.Dns, stringResource(R.string.kube_server_title), onClick = { editing = true })
                    }
                },
            )
        },
    ) { padding ->
        Column(Modifier.padding(padding).fillMaxSize()) {
            PrimaryTabRow(selectedTabIndex = tab) {
                Tab(selected = tab == 0, onClick = { tab = 0 }, text = { Text(stringResource(R.string.workloads_title)) })
                Tab(selected = tab == 1, onClick = { tab = 1 }, text = { Text(stringResource(R.string.pods_title)) })
                Tab(selected = tab == 2, onClick = { tab = 2 }, text = { Text(stringResource(R.string.cronjobs_title)) })
                Tab(selected = tab == 3, onClick = { tab = 3 }, text = { Text(stringResource(R.string.netperf_tab)) })
            }
            when (tab) {
                0 -> WorkloadsTab(namespace, query, onNamespace = { namespace = it }, onQuery = { query = it }, vm = workloads)
                1 -> PodsTab(
                    namespace,
                    query,
                    onNamespace = { namespace = it },
                    onQuery = { query = it },
                    onFlows = if (hasCilium) ({ pod -> onFlows(pod.namespace, pod.name) }) else null,
                    vm = pods,
                )
                2 -> CronJobsTab(namespace, query, onNamespace = { namespace = it }, onQuery = { query = it }, vm = cronJobs)
                else -> NetPerfTab(netPerf)
            }
        }
    }
}
