package name.levis.ichor.ui.dataservices

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Dns
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.PrimaryScrollableTabRow
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Tab
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
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.OVERVIEW
import name.levis.ichor.data.realFingerprint
import name.levis.ichor.model.ClusterOverview
import name.levis.ichor.model.DataServiceKind
import name.levis.ichor.model.DataServices
import name.levis.ichor.model.detected
import name.levis.ichor.model.likelyCauses
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.DataFreshness
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.workloads.KubeServerDialog
import name.levis.ichor.ui.components.TooltipIconButton

/**
 * Longhorn volumes, Garage clusters and CloudNativePG clusters, one tab per system the cluster
 * runs, through the Kubernetes API with the admin kubeconfig Talos issues (os:admin). A node
 * that is not ready and explains the problems is named above the tabs. Like the Kubernetes
 * screen, the top bar sets the API address (not in demo or screenshot mode).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DataServicesScreen(initial: DataServiceKind? = null, onBack: () -> Unit) {
    val app = LocalContext.current.applicationContext as TalosApp
    val vm: DataServicesViewModel = viewModel(factory = factory { DataServicesViewModel(app.talosRepository) })
    val state by vm.state.collectAsStateWithLifecycle()
    val config by app.configRepository.config.collectAsStateWithLifecycle()
    val generation by app.configRepository.generation.collectAsStateWithLifecycle()
    val invalidations by app.talosRepository.invalidations.collectAsStateWithLifecycle()
    val servers by app.kubeServers.servers.collectAsStateWithLifecycle()
    val mask by app.uiPreferences.privacyMask.collectAsStateWithLifecycle()

    LaunchedEffect(config?.activeContext, generation, invalidations) {
        vm.load(Triple(config?.activeContext, generation, invalidations), vm.inventoryHints())
    }
    GarageResultToasts(vm.garage.results)
    LonghornResultToasts(vm.longhorn.results)
    CertificateRenewToasts(vm.certificates.results)
    CnpgBackupToasts(vm.cnpg.results)

    val fingerprint = config?.realFingerprint?.takeUnless { mask.enabled }
    var editing by remember { mutableStateOf(false) }
    LaunchedEffect(fingerprint) { if (fingerprint == null) editing = false }
    if (editing && fingerprint != null) {
        KubeServerDialog(
            saved = servers[fingerprint].orEmpty(),
            onSave = { server ->
                editing = false
                if (server != servers[fingerprint].orEmpty()) {
                    app.setKubeServer(fingerprint, server)
                    vm.refresh(reset = true)
                }
            },
            onDismiss = { editing = false },
        )
    }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text(stringResource(R.string.data_services_title)) },
                navigationIcon = { BackButton(onBack) },
                actions = {
                    TooltipIconButton(Icons.Outlined.Refresh, stringResource(R.string.data_services_refresh), onClick = { vm.refresh() })
                    if (fingerprint != null) {
                        TooltipIconButton(Icons.Outlined.Dns, stringResource(R.string.kube_server_title), onClick = { editing = true })
                    }
                },
            )
        },
    ) { padding ->
        val modifier = Modifier.padding(padding)
        when (val s = state) {
            UiState.Loading -> LoadingBox(modifier)
            is UiState.Failed -> ErrorBox(s.message, vm::refresh, modifier)
            is UiState.Loaded -> {
                val downNodes = remember(s.data) {
                    app.talosRepository.cached<ClusterOverview>(OVERVIEW)?.value?.downHostnames().orEmpty()
                }
                Column(modifier.fillMaxSize()) {
                    PullToRefreshBox(isRefreshing = s.refreshing, onRefresh = vm::refresh, modifier = Modifier.weight(1f)) {
                        Systems(s.data, downNodes, initial, vm.garage, vm.longhorn, vm.certificates, vm.cnpg)
                    }
                    DataFreshness(s, edgeToEdge = false)
                }
            }
        }
    }
}

@Composable
private fun Systems(services: DataServices, downNodes: Set<String>, initial: DataServiceKind?, garage: GarageActions, longhorn: LonghornActions, certificates: CertificateActions, cnpg: CnpgActions) {
    val kinds = services.detected
    if (kinds.isEmpty()) {
        EmptyText(stringResource(R.string.data_services_none))
        return
    }
    var selected by rememberSaveable { mutableStateOf(initial) }
    val tab = selected?.takeIf { it in kinds } ?: kinds.first()
    val causes = remember(services, downNodes) { services.likelyCauses(downNodes) }

    Column(Modifier.fillMaxSize()) {
        LikelyCauseBanner(causes, Modifier.padding(horizontal = 16.dp, vertical = 8.dp))
        if (kinds.size > 1) {
            // Scrollable: a cluster can run more systems than fit across the screen.
            PrimaryScrollableTabRow(selectedTabIndex = kinds.indexOf(tab), edgePadding = 0.dp) {
                kinds.forEach { kind ->
                    Tab(selected = kind == tab, onClick = { selected = kind }, text = { Text(kind.tabTitle, maxLines = 1) })
                }
            }
        }
        when (tab) {
            DataServiceKind.LONGHORN -> LonghornTab(services.longhorn!!, longhorn, garageDetected = services.garage != null, onGarage = { selected = DataServiceKind.GARAGE })
            DataServiceKind.GARAGE -> GarageTab(services.garage!!, garage)
            DataServiceKind.CNPG -> CnpgTab(services.cnpg!!, cnpg)
            DataServiceKind.DRAGONFLY -> DragonflyTab(services.dragonfly!!)
            DataServiceKind.MARIADB -> MariaDbTab(services.mariadb!!)
            DataServiceKind.PERCONA -> PerconaTab(services.percona!!)
            DataServiceKind.CERT_MANAGER -> CertificatesTab(services.certManager!!, certificates)
            DataServiceKind.VELERO -> VeleroTab(services.velero!!)
            DataServiceKind.CEPH -> CephTab(services.ceph!!)
        }
    }
}
