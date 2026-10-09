package name.levis.ichor.ui.maintenance

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarDuration
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
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.MaintenanceManager
import name.levis.ichor.model.KubeAction
import name.levis.ichor.ui.components.rememberKubeDenial
import name.levis.ichor.data.activeIsKube
import name.levis.ichor.data.activeSummary
import name.levis.ichor.model.MaintenancePlan
import name.levis.ichor.model.isDemo
import name.levis.ichor.security.AuthResult
import name.levis.ichor.security.authenticate
import name.levis.ichor.security.findFragmentActivity
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.node.HostnameConfirmDialog
import name.levis.ichor.ui.uiText
import name.levis.ichor.ui.components.pageContent

class MaintenancePlanViewModel(private val maintenances: MaintenanceManager, private val node: String) : LoadingViewModel<MaintenancePlan>() {
    override suspend fun fetch() = maintenances.plan(node)
}

/**
 * Node maintenance: the plan, then the followed run (which goes on when leaving the screen).
 * [drainOnly]: a drain alone, without the reboot or shutdown to pick; always so on a cluster
 * without Talos ([node]: the Kubernetes node name).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun MaintenanceScreen(
    node: String,
    hostname: String,
    onBack: () -> Unit,
    drainOnly: Boolean = false,
    planVm: MaintenancePlanViewModel = viewModel(key = "maintenance-plan-$node", factory = factory { MaintenancePlanViewModel(app.maintenanceManager, node) }),
) {
    val context = LocalContext.current
    val app = context.applicationContext as TalosApp
    val maintenances = app.maintenanceManager
    val current by maintenances.current.collectAsStateWithLifecycle()
    val upgrading by app.upgradeManager.current.collectAsStateWithLifecycle()
    val config by app.configRepository.config.collectAsStateWithLifecycle()
    val plan by planVm.state.collectAsStateWithLifecycle()
    val snackbar = remember { SnackbarHostState() }
    val scope = rememberCoroutineScope()
    var confirming by remember { mutableStateOf<MaintenanceChoice?>(null) }
    val following = current?.takeIf { it.node == node }
    val draining = drainOnly || config?.activeIsKube == true
    // A cluster without Talos drains with its own credentials; a Talos one with its admin kubeconfig.
    val drainDenial = if (config?.activeIsKube == true) rememberKubeDenial(KubeAction.DRAIN_NODE, "") else null
    LaunchedEffect(Unit) { if (plan == UiState.Loading) planVm.refresh() }

    fun start(choice: MaintenanceChoice, name: String) {
        val wasCordoned = (plan as? UiState.Loaded)?.data?.cordoned == true
        scope.launch {
            val result = runCatching {
                withContext(Dispatchers.IO) { maintenances.start(node, name, choice.action, choice.includeBare, choice.acknowledged, wasCordoned) }
            }
            result.exceptionOrNull()?.let { snackbar.showSnackbar(it.uiText().resolve(context), withDismissAction = true, duration = SnackbarDuration.Long) }
            if (result.getOrNull() == false) {
                snackbar.showSnackbar(context.getString(R.string.maintenance_other_running, maintenances.current.value?.hostname.orEmpty()))
            }
        }
    }

    // Like a reboot: the typed-hostname confirmation, then a fresh fingerprint/PIN with the app lock on.
    fun confirmed(choice: MaintenanceChoice, name: String) {
        confirming = null
        val activity = context.findFragmentActivity()
        if (!app.appLock.enabled.value || activity == null) {
            start(choice, name)
            return
        }
        scope.launch {
            when (val auth = authenticate(activity, context.getString(R.string.maintenance_auth_title, name), context.getString(choice.action.label))) {
                AuthResult.Success -> start(choice, name)
                is AuthResult.Failure -> snackbar.showSnackbar(auth.message, withDismissAction = true, duration = SnackbarDuration.Long)
            }
        }
    }

    val busyWith = when {
        current?.running == true && current?.node != node -> stringResource(R.string.maintenance_other_running, current?.hostname.orEmpty())
        upgrading?.running == true -> stringResource(R.string.upgrade_other_running, upgrading?.hostname.orEmpty())
        else -> null
    }
    Scaffold(
        snackbarHost = { SnackbarHost(snackbar) },
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text(stringResource(if (draining) R.string.maintenance_phase_drain else R.string.maintenance_title))
                        Text(hostname, style = MaterialTheme.typography.labelMedium)
                    }
                },
                navigationIcon = { BackButton(onBack) },
            )
        },
    ) { padding ->
        Box(Modifier.pageContent(padding).fillMaxSize()) {
            if (following != null) {
                MaintenanceRunView(
                    following,
                    onStop = maintenances::stop,
                    onClose = {
                        maintenances.dismiss()
                        onBack()
                    },
                )
            } else {
                when (val s = plan) {
                    UiState.Loading -> LoadingBox()
                    is UiState.Failed -> ErrorBox(s.message, planVm::refresh)
                    is UiState.Loaded -> PullToRefreshBox(isRefreshing = s.refreshing, onRefresh = planVm::refresh) {
                        MaintenancePlanView(
                            plan = s.data,
                            demo = config?.activeSummary?.isDemo == true,
                            busyWith = busyWith,
                            drainOnly = draining,
                            denial = drainDenial,
                            onStart = { confirming = it },
                        )
                    }
                }
            }
        }
    }

    confirming?.let { choice ->
        val name = (plan as? UiState.Loaded)?.data?.hostname?.ifEmpty { null } ?: hostname
        HostnameConfirmDialog(
            title = if (draining) {
                stringResource(R.string.drain_confirm_title, name)
            } else {
                stringResource(R.string.maintenance_confirm_title, stringResource(choice.action.label), name)
            },
            hostname = name,
            confirmLabel = stringResource(if (draining) R.string.maintenance_phase_drain else R.string.maintenance_start),
            onConfirm = { confirmed(choice, name) },
            onDismiss = { confirming = null },
        ) {
            Text(stringResource(R.string.maintenance_confirm_body, name), style = MaterialTheme.typography.bodyMedium)
            Text(stringResource(choice.action.description), style = MaterialTheme.typography.bodyMedium)
        }
    }
}
