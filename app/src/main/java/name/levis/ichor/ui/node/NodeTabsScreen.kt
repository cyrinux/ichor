package name.levis.ichor.ui.node

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Share
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.PrimaryScrollableTabRow
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarDuration
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.data.activeSummary
import name.levis.ichor.data.servicesKey
import name.levis.ichor.model.ContainerInfo
import name.levis.ichor.model.Feature
import name.levis.ichor.model.PodSelection
import name.levis.ichor.model.ServiceInfo
import name.levis.ichor.model.ShareTarget
import name.levis.ichor.model.TalosFeature
import name.levis.ichor.model.allows
import name.levis.ichor.model.notice
import name.levis.ichor.model.support
import name.levis.ichor.security.AuthResult
import name.levis.ichor.security.authenticate
import name.levis.ichor.security.findFragmentActivity
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.components.AppTab
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.FeatureGate
import name.levis.ichor.ui.components.SwipeTabPager
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.pageContent
import name.levis.ichor.ui.components.rememberNodeFeatures
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.history.NodeUptime
import name.levis.ichor.ui.live.LiveStatsTab
import name.levis.ichor.ui.share.PinButton
import name.levis.ichor.ui.share.rememberShareLink
import name.levis.ichor.ui.workloads.SelectedPodsList

/** How long after a service action the list is fetched again, once the state settled. */
private const val SERVICE_SETTLE_MILLIS = 2_500L

class ServicesViewModel(private val talos: TalosRepository, private val node: String) : LoadingViewModel<List<ServiceInfo>>() {
    override val keepsDataOnFailure = true
    override fun cached(): TalosRepository.Timed<List<ServiceInfo>>? = talos.cached(servicesKey(node))
    override val restores get() = talos.restores
    override suspend fun fetch() = talos.services(node)
}

/**
 * The node's state, one tab each: Services, Resources, Live, Processes, Pods (Containers), then
 * Cgroups and Kubernetes pods when the role allows them. Opened from the node screen's Status
 * tiles, or straight from a link that names a tab.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun NodeTabsScreen(
    node: String,
    hostname: String,
    initialTab: Int,
    onBack: () -> Unit,
    onLogs: (service: String?) -> Unit,
    onContainerLogs: (ContainerInfo) -> Unit,
) {
    var tab by rememberSaveable { mutableIntStateOf(initialTab) }
    val snackbar = remember { SnackbarHostState() }
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val app = context.applicationContext as TalosApp
    val appLock = app.appLock
    val share = rememberShareLink()
    val config by app.configRepository.config.collectAsStateWithLifecycle()
    val canControlServices = config?.activeSummary?.allows(Feature.SERVICE_CONTROL) ?: false
    val canRestartContainers = config?.activeSummary?.allows(Feature.CONTAINER_RESTART) ?: false
    // Pressure and cgroups come from a copy of /sys/fs/cgroup: os:admin only, hidden otherwise.
    val canCgroups = config?.activeSummary?.allows(Feature.CGROUPS) ?: false
    // The node's pods as Kubernetes sees them: through the Kubernetes API, os:admin only.
    val canKubePods = config?.activeSummary?.allows(Feature.WORKLOADS) ?: false
    val features = rememberNodeFeatures(node)
    val serviceControl: ServiceControlViewModel = viewModel(
        key = "service-control-$node",
        factory = factory { ServiceControlViewModel(app.talosRepository, node) },
    )
    val services: ServicesViewModel = viewModel(key = "services-$node", factory = factory { ServicesViewModel(app.talosRepository, node) })
    val controlState by serviceControl.state.collectAsStateWithLifecycle()
    var confirmingService by remember { mutableStateOf<ServiceRequest?>(null) }
    val containerRestart: ContainerRestartViewModel = viewModel(
        key = "container-restart-$node",
        factory = factory { ContainerRestartViewModel(app.talosRepository, node) },
    )
    val restartState by containerRestart.state.collectAsStateWithLifecycle()
    var confirmingContainer by remember { mutableStateOf<ContainerInfo?>(null) }

    // The Pods tab polls every few seconds: the restarted container shows up there by itself.
    LaunchedEffect(restartState) {
        when (val s = restartState) {
            is ContainerRestartState.Done -> {
                snackbar.showSnackbar(context.getString(R.string.container_restart_done, s.container.displayName, hostname))
                containerRestart.dismiss()
            }
            is ContainerRestartState.Failed -> {
                snackbar.showSnackbar(
                    context.getString(R.string.container_restart_failed, s.container.displayName, s.message.resolve(context)),
                    withDismissAction = true,
                    duration = SnackbarDuration.Long,
                )
                containerRestart.dismiss()
            }
            else -> Unit
        }
    }

    // Like service control: with the app lock on, a fresh fingerprint/PIN first.
    fun containerRestartConfirmed(container: ContainerInfo) {
        confirmingContainer = null
        val activity = context.findFragmentActivity()
        if (!appLock.enabled.value || activity == null) {
            containerRestart.run(container)
            return
        }
        scope.launch {
            when (val auth = authenticate(activity, context.getString(R.string.container_restart), "${container.displayName} · $hostname")) {
                AuthResult.Success -> containerRestart.run(container)
                is AuthResult.Failure -> snackbar.showSnackbar(auth.message, withDismissAction = true, duration = SnackbarDuration.Long)
            }
        }
    }

    // dismiss() comes last: it changes the effect's key, which cancels whatever still runs here.
    LaunchedEffect(controlState) {
        when (val s = controlState) {
            is ServiceControlState.Done -> {
                services.refresh()
                // Talos applies the action asynchronously: look again once it had time to settle
                // (in the screen's scope, so another action does not cancel it).
                scope.launch {
                    delay(SERVICE_SETTLE_MILLIS)
                    services.refresh()
                }
                snackbar.showSnackbar(context.getString(s.request.action.done, s.request.service, hostname))
                serviceControl.dismiss()
            }
            is ServiceControlState.Failed -> {
                snackbar.showSnackbar(
                    context.getString(
                        R.string.service_action_failed,
                        context.getString(s.request.action.label),
                        s.request.service,
                        s.message.resolve(context),
                    ),
                    withDismissAction = true,
                    duration = SnackbarDuration.Long,
                )
                serviceControl.dismiss()
            }
            else -> Unit
        }
    }

    // Like power actions: with the app lock on, a fresh fingerprint/PIN first.
    fun serviceConfirmed(request: ServiceRequest) {
        confirmingService = null
        val activity = context.findFragmentActivity()
        if (!appLock.enabled.value || activity == null) {
            serviceControl.run(request)
            return
        }
        scope.launch {
            val title = context.getString(request.action.label)
            when (val auth = authenticate(activity, title, "${request.service} · $hostname")) {
                AuthResult.Success -> serviceControl.run(request)
                is AuthResult.Failure -> snackbar.showSnackbar(auth.message, withDismissAction = true, duration = SnackbarDuration.Long)
            }
        }
    }

    Scaffold(
        snackbarHost = { SnackbarHost(snackbar) },
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text(hostname)
                        Text(node, style = MaterialTheme.typography.labelMedium, fontFamily = FontFamily.Monospace)
                    }
                },
                navigationIcon = { BackButton(onBack) },
                actions = {
                    if (controlState is ServiceControlState.Running || restartState is ContainerRestartState.Running) {
                        CircularProgressIndicator(Modifier.size(20.dp).padding(end = 4.dp), strokeWidth = 2.dp)
                    }
                    // Without the tab: the pin opens the node, like the overview's rows.
                    PinButton(ShareTarget(target = ShareTarget.NODE, addr = node, host = hostname), hostname)
                    TooltipIconButton(Icons.Outlined.Share, stringResource(R.string.share_link), onClick = {
                        share(ShareTarget.node(node, hostname, tab))
                    })
                },
            )
        },
    ) { padding ->
        Column(Modifier.pageContent(padding).fillMaxSize()) {
            // Cgroups (admin configs) and Kubernetes pods (with an API server) may be hidden.
            val tabs = nodeTabs(canCgroups, canKubePods)
            val shownTab = shownNodeTab(tab, tabs)
            NodeUptime(node, Modifier.padding(horizontal = 16.dp, vertical = 4.dp))
            PrimaryScrollableTabRow(selectedTabIndex = tabs.indexOf(shownTab), edgePadding = 0.dp) {
                AppTab(selected = tab == 0, onClick = { tab = 0 }, text = { Text(stringResource(R.string.node_tab_services)) })
                AppTab(selected = tab == 1, onClick = { tab = 1 }, text = { Text(stringResource(R.string.node_tab_resources)) })
                AppTab(selected = tab == 2, onClick = { tab = 2 }, text = { Text(stringResource(R.string.node_tab_live)) })
                // Tabs this Talos version lacks stay reachable (dimmed): they say what they need.
                val dimmed = MaterialTheme.colorScheme.onSurfaceVariant.copy(alpha = 0.5f)
                AppTab(
                    selected = tab == 3,
                    onClick = { tab = 3 },
                    text = { Text(stringResource(R.string.node_tab_processes)) },
                    unselectedContentColor = if (features.support(TalosFeature.PROCESSES).supported) MaterialTheme.colorScheme.onSurfaceVariant else dimmed,
                )
                AppTab(
                    selected = tab == 4,
                    onClick = { tab = 4 },
                    // Next to the Kubernetes pods tab, the Talos (CRI) view is named after what it shows.
                    text = { Text(stringResource(if (canKubePods) R.string.apps_stat_containers else R.string.node_tab_pods)) },
                    unselectedContentColor = if (features.support(TalosFeature.CONTAINERS).supported) MaterialTheme.colorScheme.onSurfaceVariant else dimmed,
                )
                if (canCgroups) {
                    AppTab(selected = tab == CGROUPS_TAB, onClick = { tab = CGROUPS_TAB }, text = { Text(stringResource(R.string.node_tab_cgroups)) })
                }
                if (canKubePods) {
                    AppTab(selected = tab == KUBE_PODS_TAB, onClick = { tab = KUBE_PODS_TAB }, text = { Text(stringResource(R.string.node_tab_kube_pods)) })
                }
            }
            SwipeTabPager(tabs, shownTab, onSelect = { tab = it }) { page ->
                when (page) {
                    0 -> ServicesTab(
                        node,
                        onService = { onLogs(it) },
                        onAction = if (canControlServices) ({ confirmingService = it }) else null,
                        busy = controlState is ServiceControlState.Running,
                        actionsNotice = features.support(TalosFeature.SERVICE_CONTROL).notice,
                        vm = services,
                    )
                    1 -> ResourcesTab(node, onPressureDetails = if (canCgroups) ({ tab = CGROUPS_TAB }) else null)
                    2 -> LiveStatsTab(node)
                    3 -> FeatureGate(features.support(TalosFeature.PROCESSES)) { ProcessesTab(node) }
                    CGROUPS_TAB -> CgroupsTab(node)
                    KUBE_PODS_TAB -> SelectedPodsList(PodSelection.OnNode(node))
                    else -> FeatureGate(features.support(TalosFeature.CONTAINERS)) {
                        PodsTab(
                            node,
                            onContainer = onContainerLogs,
                            onRestart = if (canRestartContainers) ({ confirmingContainer = it }) else null,
                            restartSupport = features.support(TalosFeature.CONTAINER_RESTART),
                            restartBusy = restartState is ContainerRestartState.Running,
                        )
                    }
                }
            }
        }
    }

    confirmingService?.let { request ->
        ServiceConfirmDialog(
            request = request,
            hostname = hostname,
            onConfirm = { serviceConfirmed(request) },
            onDismiss = { confirmingService = null },
        )
    }

    confirmingContainer?.let { container ->
        ContainerRestartDialog(
            container = container,
            hostname = hostname,
            onConfirm = { containerRestartConfirmed(container) },
            onDismiss = { confirmingContainer = null },
        )
    }
}
