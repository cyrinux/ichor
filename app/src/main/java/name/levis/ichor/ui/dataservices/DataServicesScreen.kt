package name.levis.ichor.ui.dataservices

import name.levis.ichor.setKubeServer
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Dns
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.PrimaryScrollableTabRow
import androidx.compose.material3.Scaffold
import name.levis.ichor.ui.components.AppTab
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
import name.levis.ichor.data.KUBE_NODES
import name.levis.ichor.data.OVERVIEW
import name.levis.ichor.data.activeIsKube
import name.levis.ichor.data.realFingerprint
import name.levis.ichor.model.ClusterOverview
import name.levis.ichor.model.KubeNodesOverview
import name.levis.ichor.model.AllServiceFamilies
import name.levis.ichor.model.DataServiceKind
import name.levis.ichor.model.DataServices
import name.levis.ichor.model.KubeObjectRef
import name.levis.ichor.model.PluginDetectItem
import name.levis.ichor.model.ServiceFamily
import name.levis.ichor.model.title
import name.levis.ichor.model.detected
import name.levis.ichor.model.likelyCauses
import name.levis.ichor.model.notReadyNames
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.Loaded
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.workloads.KubeServerDialog
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.SwipeTabPager
import name.levis.ichor.ui.components.SearchField
import name.levis.ichor.ui.components.pageContent
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.plugins.PluginDetectViewModel
import name.levis.ichor.ui.plugins.PluginReload
import name.levis.ichor.ui.plugins.PluginTab
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.plugins.nativePluginRoute

/**
 * Longhorn volumes, Garage clusters and CloudNativePG clusters, one tab per system the cluster
 * runs, through the Kubernetes API with the admin kubeconfig Talos issues (os:admin). A node
 * that is not ready and explains the problems is named above the tabs. Like the Kubernetes
 * screen, the top bar sets the API address (not in demo or screenshot mode).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DataServicesScreen(
    initial: DataServiceKind? = null,
    /** A plugin's tab to open on instead (its id), for a plugin tile. */
    initialPlugin: String? = null,
    /** [initial] alone, titled by its name: a system the Kubernetes home files in another family. */
    solo: Boolean = false,
    onBack: () -> Unit,
    onObject: (KubeObjectRef) -> Unit,
    onSupportedIntegrations: () -> Unit,
    onRequestIntegration: () -> Unit,
    onNodes: (() -> Unit)? = null,
) {
    val app = LocalContext.current.applicationContext as TalosApp
    val vm: DataServicesViewModel = viewModel(factory = factory { DataServicesViewModel(app.dataServicesRepository) })
    val state by vm.state.collectAsStateWithLifecycle()
    val progress by vm.readProgress.collectAsStateWithLifecycle()
    val config by app.configRepository.config.collectAsStateWithLifecycle()
    val generation by app.configRepository.generation.collectAsStateWithLifecycle()
    val invalidations by app.talosRepository.invalidations.collectAsStateWithLifecycle()
    val servers by app.kubeServers.servers.collectAsStateWithLifecycle()
    val mask by app.uiPreferences.privacyMask.collectAsStateWithLifecycle()
    // The CRD plugins of the data-services category: one tab each, after the native systems.
    val pluginsVm: PluginDetectViewModel = viewModel(key = "data-services-plugins", factory = factory { PluginDetectViewModel(app.pluginRepository) })
    val plugins by pluginsVm.state.collectAsStateWithLifecycle()
    var refreshes by rememberSaveable { mutableIntStateOf(0) }

    LaunchedEffect(config?.activeContext, generation, invalidations) {
        val key = Triple(config?.activeContext, generation, invalidations)
        vm.load(key, vm.inventoryHints())
        pluginsVm.load(key, vm.inventoryHints())
    }
    val refreshAll: () -> Unit = {
        vm.refresh()
        pluginsVm.refresh()
        refreshes++
    }
    // A plugin tab reads again on a refresh or another cluster.
    val pluginReload = PluginReload(scope = config?.activeContext to generation, bump = listOf(invalidations, refreshes))
    val pluginTabs = (plugins as? UiState.Loaded)?.data?.dataServicePlugins.orEmpty().filter { nativePluginRoute(it) == null }
    GarageResultToasts(vm.garage.results)
    LonghornResultToasts(vm.longhorn.results)
    LonghornSalvageToasts(vm.longhorn.salvageResults)
    LonghornAttachmentToasts(vm.longhorn.attachmentResults)
    LonghornEngineToasts(vm.longhorn.engineResults)
    LonghornSnapshotToasts(vm.longhorn.snapshotResults)
    CertificateRenewToasts(vm.certificates.results)
    CnpgBackupToasts(vm.cnpg.results)
    CnpgPromoteToasts(vm.cnpg.promoteResults)
    CnpgLifecycleToasts(vm.cnpg.lifecycleResults)
    VeleroResultToasts(vm.velero.results)

    val fingerprint = config?.realFingerprint?.takeUnless { mask.enabled }
    var editing by remember { mutableStateOf(false) }
    // The top bar's search: for now it filters the Longhorn volumes.
    var searching by rememberSaveable { mutableStateOf(false) }
    var query by rememberSaveable { mutableStateOf("") }
    val searchable = state.let { it is UiState.Loaded && it.data.longhorn != null }
    LaunchedEffect(fingerprint) { if (fingerprint == null) editing = false }
    if (editing && fingerprint != null) {
        KubeServerDialog(
            saved = servers[fingerprint].orEmpty(),
            onSave = { server ->
                editing = false
                if (server != servers[fingerprint].orEmpty()) {
                    app.setKubeServer(fingerprint, server)
                    vm.refresh(reset = true)
                    pluginsVm.refresh(reset = true)
                    refreshes++
                }
            },
            onDismiss = { editing = false },
        )
    }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text(initial?.takeIf { solo }?.title ?: stringResource(R.string.data_services_title)) },
                navigationIcon = { BackButton(onBack) },
                actions = {
                    if (searchable) {
                        TooltipIconButton(Icons.Outlined.Search, stringResource(R.string.data_services_search), onClick = {
                            searching = !searching
                            if (!searching) query = ""
                        })
                    }
                    TooltipIconButton(Icons.Outlined.Refresh, stringResource(R.string.data_services_refresh), onClick = refreshAll)
                    if (fingerprint != null) {
                        TooltipIconButton(Icons.Outlined.Dns, stringResource(R.string.kube_server_title), onClick = { editing = true })
                    }
                },
            )
        },
    ) { padding ->
        val modifier = Modifier.pageContent(padding)
        Loaded(state, refreshAll, modifier, freshness = true, progress = progress, stepLabel = { dataServicesStepLabel(it) }) { data ->
            // The nodes that are not ready, as the home last saw them: the Talos overview's, or
            // the Kubernetes home's on a cluster added from a kubeconfig.
            val kube = config?.activeIsKube == true
            val downNodes = remember(data, kube) {
                if (kube) app.talosRepository.cached<KubeNodesOverview>(KUBE_NODES)?.value?.notReadyNames().orEmpty()
                else app.talosRepository.cached<ClusterOverview>(OVERVIEW)?.value?.downHostnames().orEmpty()
            }
            val tabs = remember(data, pluginTabs, kube, solo, initial) { hubTabs(data, pluginTabs, kube, initial?.takeIf { solo }) }
            val first = initialPlugin?.let { HubTab.Plugin.idOf(it) } ?: initial?.let { HubTab.Native(it).id }
            when {
                tabs.isNotEmpty() -> Systems(
                    data, tabs, downNodes, first, onObject, onNodes, query.takeIf { searching }, { query = it },
                    SystemActions(vm.garage, vm.longhorn, vm.certificates, vm.cnpg, vm.velero),
                    pluginReload,
                )
                // The plugins may still name one.
                plugins is UiState.Loading -> LoadingBox()
                // No native system, and the plugins could not be asked: say why, not "none".
                plugins is UiState.Failed -> ErrorBox((plugins as UiState.Failed).message, refreshAll)
                else -> NoDataServices(onSupportedIntegrations, onRequestIntegration)
            }
        }
    }
}

/** None of the systems the app reads runs here: which ones it reads, and how to ask for another. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun NoDataServices(onSupportedIntegrations: () -> Unit, onRequestIntegration: () -> Unit) {
    // Scrollable, so the pull-to-refresh around it still reacts.
    Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Text(stringResource(R.string.data_services_none), style = MaterialTheme.typography.titleMedium)
        MutedText(stringResource(R.string.data_services_none_hint))
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            OutlinedButton(onClick = onSupportedIntegrations) { Text(stringResource(R.string.supported_integrations_title)) }
            OutlinedButton(onClick = onRequestIntegration) { Text(stringResource(R.string.settings_integrations_open)) }
        }
    }
}

/** A tab of the hub: a native system, or a CRD plugin drawn from its render tree. */
internal sealed interface HubTab {
    /** Saved across recompositions and process death: "LONGHORN", "plugin:kube-green". */
    val id: String

    data class Native(val kind: DataServiceKind) : HubTab {
        override val id: String get() = kind.name
    }

    data class Plugin(val pluginId: String, val name: String) : HubTab {
        override val id: String get() = idOf(pluginId)

        companion object {
            fun idOf(pluginId: String) = "plugin:$pluginId"
        }
    }
}

/**
 * The hub's tabs: [solo]'s alone when it runs; else, on a Kubernetes cluster (whose home files
 * the other systems in their families), storage, databases and backups; on a Talos cluster, which
 * has no tools card yet, every system. The data-services plugins follow, except with [solo].
 */
internal fun hubTabs(services: DataServices, plugins: List<PluginDetectItem>, kube: Boolean, solo: DataServiceKind?): List<HubTab> {
    if (solo != null) return listOfNotNull(HubTab.Native(solo).takeIf { solo in services.detected })
    val families = if (kube) setOf(ServiceFamily.DATA) else AllServiceFamilies
    return services.detected(families).map { HubTab.Native(it) } + plugins.map { HubTab.Plugin(it.id, it.name) }
}

/** The pager's pages: the tabs' ids, saveable in a Bundle (a page key must be). */
internal fun hubPages(tabs: List<HubTab>): List<String> = tabs.map { it.id }

/** The actions of the native tabs, run in the screen's ViewModel. */
private class SystemActions(
    val garage: GarageActions,
    val longhorn: LonghornActions,
    val certificates: CertificateActions,
    val cnpg: CnpgActions,
    val velero: VeleroActions,
)

@Composable
private fun Systems(
    services: DataServices,
    tabs: List<HubTab>,
    downNodes: Set<String>,
    initial: String?,
    onObject: (KubeObjectRef) -> Unit,
    onNodes: (() -> Unit)?,
    query: String?,
    onQuery: (String) -> Unit,
    actions: SystemActions,
    pluginReload: PluginReload,
) {
    // Picked once: a system detected later, before it in the list, doesn't take its place.
    var selected by rememberSaveable { mutableStateOf(initial ?: tabs.first().id) }
    val tab = tabs.firstOrNull { it.id == selected } ?: tabs.first()
    val causes = remember(services, downNodes) { services.likelyCauses(downNodes) }

    Column(Modifier.fillMaxSize()) {
        LikelyCauseBanner(causes, Modifier.padding(horizontal = 16.dp, vertical = 8.dp), onNodes)
        // Shown by the top bar's search action; only the Longhorn tab filters by it yet.
        if (query != null && tab == HubTab.Native(DataServiceKind.LONGHORN)) {
            SearchField(query, onQuery, stringResource(R.string.data_services_search), Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp))
        }
        if (tabs.size > 1) {
            // Scrollable: a cluster can run more systems than fit across the screen.
            PrimaryScrollableTabRow(selectedTabIndex = tabs.indexOf(tab), edgePadding = 0.dp) {
                tabs.forEach { t ->
                    val label = when (t) {
                        is HubTab.Native -> t.kind.tabLabel()
                        is HubTab.Plugin -> t.name
                    }
                    AppTab(selected = t == tab, onClick = { selected = t.id }, text = { Text(label, maxLines = 1) })
                }
            }
        }
        // Paged by id: the pager keeps its page keys in a Bundle, which takes a String, not a HubTab.
        SwipeTabPager(hubPages(tabs), tab.id, onSelect = { selected = it }) { id ->
            when (val page = tabs.firstOrNull { it.id == id } ?: tab) {
                is HubTab.Native -> NativeTab(page.kind, services, actions, onObject, query) { selected = HubTab.Native(DataServiceKind.GARAGE).id }
                is HubTab.Plugin -> PluginTab(page.pluginId, pluginReload, onObject)
            }
        }
    }
}

@Composable
private fun NativeTab(kind: DataServiceKind, services: DataServices, actions: SystemActions, onObject: (KubeObjectRef) -> Unit, query: String?, onGarage: () -> Unit) {
    when (kind) {
        DataServiceKind.LONGHORN -> LonghornTab(services.longhorn!!, actions.longhorn, garageDetected = services.garage != null, onGarage = onGarage, query = query.orEmpty())
        DataServiceKind.GARAGE -> GarageTab(services.garage!!, actions.garage, onObject)
        DataServiceKind.CNPG -> CnpgTab(services.cnpg!!, actions.cnpg)
        DataServiceKind.CERT_MANAGER -> CertificatesTab(services.certManager!!, actions.certificates)
        DataServiceKind.VELERO -> VeleroTab(services.velero!!, actions.velero)
        DataServiceKind.SNAPSHOTS -> SnapshotsTab(services.snapshots!!)
        DataServiceKind.BACKUP_JOBS -> BackupJobsTab(services.backupJobs!!)
        DataServiceKind.CASTAI -> CastAITab(services.castai!!)
    }
}
