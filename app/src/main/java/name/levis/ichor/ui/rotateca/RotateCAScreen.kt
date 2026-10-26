package name.levis.ichor.ui.rotateca

import androidx.compose.foundation.layout.Column
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
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
import androidx.compose.ui.text.font.FontFamily
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.activeSummary
import name.levis.ichor.model.Feature
import name.levis.ichor.model.allows
import name.levis.ichor.security.AuthResult
import name.levis.ichor.security.authenticate
import name.levis.ichor.security.findFragmentActivity
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.ConfirmDialog
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.RoleNotice
import name.levis.ichor.ui.components.pageContent
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.node.HostnameConfirmDialog

/**
 * Rotates the Talos API CA of the cluster on screen (os:admin), like `talosctl rotate-ca
 * --kubernetes=false`: the plan first (nodes, the current CA, what stands in the way), then
 * the run, followed in a notification. [onIssueConfig] opens Issue talosconfig, to give the
 * other devices a config of the new CA once it is done.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun RotateCAScreen(
    onBack: () -> Unit,
    onIssueConfig: () -> Unit,
    vm: RotateCAViewModel = viewModel(factory = factory { RotateCAViewModel(app.nodeLifecycleRepository, app.caRotationManager) }),
) {
    val context = LocalContext.current
    val talosApp = context.applicationContext as TalosApp
    val config by talosApp.configRepository.config.collectAsStateWithLifecycle()
    val summary = config?.activeSummary
    val plan by vm.plan.collectAsStateWithLifecycle()
    val run by vm.run.collectAsStateWithLifecycle()
    val startError by vm.startError.collectAsStateWithLifecycle()
    val scope = rememberCoroutineScope()
    var confirmStart by remember { mutableStateOf(false) }
    var confirmStop by remember { mutableStateOf(false) }
    LaunchedEffect(Unit) { if (plan == UiState.Loading) vm.refresh() }

    // With the app lock on, a fresh fingerprint/PIN before the cluster's CA changes.
    fun start() {
        val activity = context.findFragmentActivity()
        if (!talosApp.appLock.enabled.value || activity == null) {
            vm.start()
            return
        }
        scope.launch {
            when (val auth = authenticate(activity, context.getString(R.string.ca_rotate_auth_title), summary?.name.orEmpty())) {
                AuthResult.Success -> vm.start()
                is AuthResult.Failure -> vm.failStart(UiText.Raw(auth.message))
            }
        }
    }

    Scaffold(
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text(stringResource(R.string.ca_rotate_title))
                        summary?.let { Text(it.name, style = MaterialTheme.typography.labelMedium, fontFamily = FontFamily.Monospace) }
                    }
                },
                navigationIcon = { BackButton(onBack) },
            )
        },
    ) { padding ->
        val current = run
        when {
            summary == null -> Unit
            !summary.allows(Feature.ISSUE_CONFIG) -> Column(Modifier.pageContent(padding)) { RoleNotice(Feature.ISSUE_CONFIG, summary) }
            // The run is the screen while it goes on, and once it ended until dismissed.
            current != null -> RunContent(
                run = current,
                onScreen = summary.name,
                modifier = Modifier.pageContent(padding),
                onStop = { confirmStop = true },
                onIssueConfig = onIssueConfig,
                onDone = vm::dismiss,
            )
            else -> when (val s = plan) {
                UiState.Loading -> LoadingBox(Modifier.pageContent(padding))
                is UiState.Failed -> ErrorBox(s.message, vm::refresh, Modifier.pageContent(padding))
                is UiState.Loaded -> PlanContent(
                    plan = s.data,
                    refreshing = s.refreshing,
                    startError = startError,
                    modifier = Modifier.pageContent(padding),
                    onRefresh = vm::refresh,
                    onStart = { confirmStart = true },
                )
            }
        }
    }

    if (confirmStart && summary != null) {
        HostnameConfirmDialog(
            title = stringResource(R.string.ca_rotate_confirm_title, summary.name),
            hostname = summary.name,
            confirmLabel = stringResource(R.string.ca_rotate_confirm_action),
            emphasized = true,
            onConfirm = {
                confirmStart = false
                start()
            },
            onDismiss = { confirmStart = false },
        ) {
            Text(stringResource(R.string.ca_rotate_confirm_body))
        }
    }
    if (confirmStop) {
        ConfirmDialog(
            title = stringResource(R.string.ca_rotate_stop_title),
            text = stringResource(R.string.ca_rotate_stop_body),
            confirm = stringResource(R.string.operation_stop),
            onConfirm = {
                confirmStop = false
                vm.stop()
            },
            onDismiss = { confirmStop = false },
        )
    }
}
