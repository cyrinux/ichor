package name.levis.ichor.ui.upgrade

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarDuration
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.compose.viewModel
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.data.activeSummary
import name.levis.ichor.model.isDemo
import name.levis.ichor.data.UpgradeManager
import name.levis.ichor.model.ClusterUpgradePlan
import name.levis.ichor.model.TalosRelease
import name.levis.ichor.model.releaseSuggestions
import name.levis.ichor.security.AuthResult
import name.levis.ichor.security.authenticate
import name.levis.ichor.security.findFragmentActivity
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.pageContent
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.node.HostnameConfirmDialog
import name.levis.ichor.ui.uiStateOf
import name.levis.ichor.ui.uiText

/** The version picked and its plan; releases come from the network and may be missing offline. */
class ClusterUpgradeViewModel(
    private val talos: TalosRepository,
    private val upgrades: UpgradeManager,
    initialVersion: String,
) : ViewModel() {
    private val _releases = MutableStateFlow<UiState<List<TalosRelease>>>(UiState.Loading)
    val releases: StateFlow<UiState<List<TalosRelease>>> = _releases.asStateFlow()
    private val _version = MutableStateFlow(initialVersion)
    val version: StateFlow<String> = _version.asStateFlow()
    private val _plan = MutableStateFlow<UiState<ClusterUpgradePlan>?>(null)
    val plan: StateFlow<UiState<ClusterUpgradePlan>?> = _plan.asStateFlow()
    private var planning: Job? = null

    init {
        viewModelScope.launch {
            val loaded = uiStateOf { releaseSuggestions(upgrades.releases(), includePrerelease = false) }
            _releases.value = loaded
            // The overview's version when it offered one, else the newest stable release.
            if (_version.value.isEmpty()) (loaded as? UiState.Loaded)?.data?.firstOrNull()?.let { pick(it.version) }
        }
        if (initialVersion.isNotEmpty()) pick(initialVersion)
    }

    fun pick(version: String) {
        _version.value = version
        refresh()
    }

    fun refresh() {
        val version = _version.value.ifEmpty { return }
        planning?.cancel()
        _plan.value = UiState.Loading
        planning = viewModelScope.launch { _plan.value = uiStateOf { talos.clusterUpgradePlan(version) } }
    }
}

/**
 * Upgrade cluster: the plan (version, ordered nodes, checks), then the followed roll, which
 * goes on when leaving the screen. [initialVersion]: the version the overview offered ("": the
 * newest stable release).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ClusterUpgradeScreen(
    initialVersion: String,
    onBack: () -> Unit,
    vm: ClusterUpgradeViewModel = viewModel(
        key = "cluster-upgrade",
        factory = factory { ClusterUpgradeViewModel(app.talosRepository, app.upgradeManager, initialVersion) },
    ),
) {
    val context = LocalContext.current
    val app = context.applicationContext as TalosApp
    val rolls = app.clusterUpgradeManager
    val current by rolls.current.collectAsStateWithLifecycle()
    val upgrading by app.upgradeManager.current.collectAsStateWithLifecycle()
    val maintenance by app.maintenanceManager.current.collectAsStateWithLifecycle()
    val config by app.configRepository.config.collectAsStateWithLifecycle()
    val releases by vm.releases.collectAsStateWithLifecycle()
    val version by vm.version.collectAsStateWithLifecycle()
    val plan by vm.plan.collectAsStateWithLifecycle()
    val snackbar = remember { SnackbarHostState() }
    val scope = rememberCoroutineScope()
    var confirming by remember { mutableStateOf<ClusterUpgradeChoice?>(null) }
    // What is typed to confirm: the cluster, as its context is named.
    val cluster = config?.activeContext.orEmpty()

    fun start(choice: ClusterUpgradeChoice) {
        scope.launch {
            val result = runCatching { withContext(Dispatchers.IO) { rolls.start(choice.version, cluster, choice.drain, choice.acknowledged) } }
            result.exceptionOrNull()?.let { snackbar.showSnackbar(it.uiText().resolve(context), withDismissAction = true, duration = SnackbarDuration.Long) }
        }
    }

    // Like an upgrade: the typed confirmation, then a fresh fingerprint/PIN with the app lock on.
    fun confirmed(choice: ClusterUpgradeChoice) {
        confirming = null
        val activity = context.findFragmentActivity()
        if (!app.appLock.enabled.value || activity == null) {
            start(choice)
            return
        }
        scope.launch {
            when (val auth = authenticate(activity, context.getString(R.string.cluster_upgrade_auth, cluster), choice.version)) {
                AuthResult.Success -> start(choice)
                is AuthResult.Failure -> snackbar.showSnackbar(auth.message, withDismissAction = true, duration = SnackbarDuration.Long)
            }
        }
    }

    val busyWith = when {
        upgrading?.running == true -> stringResource(R.string.upgrade_other_running, upgrading?.hostname.orEmpty())
        maintenance?.running == true -> stringResource(R.string.maintenance_other_running, maintenance?.hostname.orEmpty())
        else -> null
    }
    Scaffold(
        snackbarHost = { SnackbarHost(snackbar) },
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text(stringResource(R.string.cluster_upgrade_title))
                        Text(cluster, style = MaterialTheme.typography.labelMedium)
                    }
                },
                navigationIcon = { BackButton(onBack) },
            )
        },
    ) { padding ->
        Box(Modifier.pageContent(padding).fillMaxSize()) {
            val run = current
            if (run != null) {
                ClusterUpgradeRunView(
                    run,
                    onPause = rolls::pause,
                    onResume = rolls::resume,
                    onAbort = rolls::abort,
                    onClose = {
                        rolls.dismiss()
                        vm.refresh()
                    },
                )
            } else {
                ClusterUpgradePlanView(
                    releases = releases,
                    version = version,
                    onVersion = vm::pick,
                    plan = plan,
                    onRetry = vm::refresh,
                    demo = config?.activeSummary?.isDemo == true,
                    busyWith = busyWith,
                    onStart = { confirming = it },
                )
            }
        }
    }

    confirming?.let { choice ->
        HostnameConfirmDialog(
            title = stringResource(R.string.cluster_upgrade_confirm_title, cluster, choice.version),
            hostname = cluster,
            confirmLabel = stringResource(R.string.cluster_upgrade_start),
            onConfirm = { confirmed(choice) },
            onDismiss = { confirming = null },
            emphasized = true,
        ) {
            Text(stringResource(R.string.cluster_upgrade_confirm_body), style = MaterialTheme.typography.bodyMedium)
        }
    }
}
