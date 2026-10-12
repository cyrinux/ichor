package name.levis.ichor.ui.join

import androidx.annotation.StringRes
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
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
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.JoinRequest
import name.levis.ichor.data.JoinRunState
import name.levis.ichor.model.JoinPhase
import name.levis.ichor.model.joinTimeline
import name.levis.ichor.security.AuthResult
import name.levis.ichor.security.authenticate
import name.levis.ichor.security.findFragmentActivity
import name.levis.ichor.ui.components.ConfirmDialog
import name.levis.ichor.ui.components.KeepScreenOn
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.ToggleRow
import name.levis.ichor.ui.node.HostnameConfirmDialog
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.upgrade.TimelineRow

/**
 * Step 3 of adding a node: what is about to happen, the warnings, then "Join" behind the typed
 * hostname (and the app lock); the followed run's timeline replaces it once started. [request]
 * is null until steps 1 and 2 are done (a node in maintenance mode, a valid config).
 */
@Composable
fun JoinApplyStep(request: JoinRequest?, onOpenNode: (address: String, hostname: String, role: String) -> Unit) {
    val app = LocalContext.current.applicationContext as TalosApp
    val run by app.joinManager.current.collectAsStateWithLifecycle()
    SectionTitle(stringResource(R.string.join_node_step_apply))
    val current = run
    when {
        current != null -> JoinRunView(
            run = current,
            onStop = app.joinManager::stop,
            onOpenNode = { onOpenNode(current.address, current.request.hostname, current.request.role) },
            onClose = app.joinManager::dismiss,
        )
        request == null -> MutedText(stringResource(R.string.join_apply_waiting))
        else -> JoinSummary(request) { app.joinManager.start(it) }
    }
}

@Composable
private fun JoinSummary(request: JoinRequest, onJoin: (JoinRequest) -> Unit) {
    val context = LocalContext.current
    val app = context.applicationContext as TalosApp
    val scope = rememberCoroutineScope()
    var warnings by remember(request.role) { mutableStateOf<List<String>>(emptyList()) }
    var confirm by remember { mutableStateOf(false) }
    var replacing by remember(request.hostname) { mutableStateOf(false) }
    LaunchedEffect(request.role) {
        warnings = runCatching { app.talosRepository.nodeJoinWarnings(request.role) }.getOrDefault(emptyList())
    }

    fun authenticated(action: () -> Unit) {
        val activity = context.findFragmentActivity()
        if (!app.appLock.enabled.value || activity == null) {
            action()
            return
        }
        scope.launch {
            if (authenticate(activity, context.getString(R.string.join_apply_confirm_title, request.hostname)) is AuthResult.Success) action()
        }
    }

    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            SummaryRow(R.string.join_apply_address, request.address)
            SummaryRow(R.string.join_apply_role, if (request.role == "controlplane") stringResource(R.string.overview_role_control_plane) else request.role)
            SummaryRow(R.string.join_config_hostname, request.hostname)
            SummaryRow(R.string.join_config_disk, request.disk)
            SummaryRow(R.string.join_config_template, request.template)
            if (request.staticAddress.isNotBlank()) SummaryRow(R.string.join_config_address, request.staticAddress)
            warnings.forEach { Text("• $it", style = MaterialTheme.typography.bodySmall, color = LocalStatusColors.current.warn) }
            ToggleRow(
                title = stringResource(R.string.join_apply_replacing),
                checked = replacing,
                onChange = { replacing = it },
                description = stringResource(R.string.join_apply_replacing_help, request.hostname),
            )
            Button(onClick = { confirm = true }, modifier = Modifier.fillMaxWidth()) {
                Text(stringResource(R.string.join_apply_join))
            }
        }
    }

    if (confirm) {
        HostnameConfirmDialog(
            title = stringResource(R.string.join_apply_confirm_title, request.hostname),
            hostname = request.hostname,
            confirmLabel = stringResource(R.string.join_apply_join),
            onConfirm = {
                confirm = false
                authenticated { onJoin(request.copy(replacing = replacing)) }
            },
            onDismiss = { confirm = false },
            emphasized = true,
        ) {
            Text(stringResource(R.string.join_apply_confirm_body, request.address, request.disk), style = MaterialTheme.typography.bodyMedium)
        }
    }
}

@Composable
private fun SummaryRow(@StringRes label: Int, value: String) {
    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        Text(stringResource(label), style = MaterialTheme.typography.bodySmall, modifier = Modifier.weight(0.4f))
        Text(value, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace, modifier = Modifier.weight(0.6f))
    }
}

/** The followed join: phase timeline, then the result and "Open node". */
@Composable
private fun JoinRunView(run: JoinRunState, onStop: () -> Unit, onOpenNode: () -> Unit, onClose: () -> Unit) {
    val colors = LocalStatusColors.current
    var confirmStop by remember { mutableStateOf(false) }
    val steps = remember(run.events, run.finished, run.error) { joinTimeline(run.events, run.finished, run.error != null) }
    if (run.running) KeepScreenOn()

    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            steps.forEach { TimelineRow(stringResource(it.phase.label), it.status, it.at, it.message) }
        }
    }
    when {
        run.running -> {
            MutedText(stringResource(R.string.join_run_stop_hint))
            OutlinedButton(onClick = { confirmStop = true }, modifier = Modifier.fillMaxWidth()) {
                Text(stringResource(R.string.join_run_stop))
            }
        }
        run.error != null -> {
            Text(run.error, color = colors.bad)
            Button(onClick = onClose, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.common_ok)) }
        }
        else -> {
            Text(stringResource(R.string.join_run_done, run.hostname, run.address), color = colors.ok, style = MaterialTheme.typography.titleMedium)
            Button(
                onClick = {
                    onClose()
                    onOpenNode()
                },
                modifier = Modifier.fillMaxWidth(),
            ) { Text(stringResource(R.string.join_run_open_node)) }
        }
    }

    if (confirmStop) {
        ConfirmDialog(
            title = stringResource(R.string.join_run_stop),
            text = stringResource(R.string.join_run_stop_body, run.hostname),
            confirm = stringResource(R.string.join_run_stop),
            onConfirm = {
                confirmStop = false
                onStop()
            },
            onDismiss = { confirmStop = false },
            destructive = true,
        )
    }
}

/** Label of the step in the join timeline. */
@get:StringRes
private val JoinPhase.label: Int
    get() = when (this) {
        JoinPhase.APPLYING -> R.string.join_phase_applying
        JoinPhase.INSTALLING -> R.string.join_phase_installing
        JoinPhase.REBOOTING -> R.string.join_phase_rebooting
        JoinPhase.BOOTED -> R.string.join_phase_booted
        JoinPhase.JOINING -> R.string.join_phase_joining
        JoinPhase.REGISTERING -> R.string.join_phase_registering
        JoinPhase.DONE -> R.string.join_phase_done
    }
