package name.levis.ichor.ui.node

import androidx.annotation.StringRes
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.selection.selectableGroup
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.unit.dp
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.model.NodeResetPlan
import name.levis.ichor.model.ResetRequest
import name.levis.ichor.model.ResetWipe
import name.levis.ichor.model.allowed
import name.levis.ichor.model.leavesDeadMember
import name.levis.ichor.model.wipeModes
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.components.ToggleRow
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.uiStateOf
import name.levis.ichor.ui.uiText

sealed interface ResetState {
    data object Idle : ResetState
    data object Running : ResetState
    data object Done : ResetState
    data class Failed(val message: UiText) : ResetState
}

/** The reset plan of [node] and the reset itself (`talosctl reset`). */
class ResetViewModel(private val talos: TalosRepository, private val node: String) : ViewModel() {
    private val _plan = MutableStateFlow<UiState<NodeResetPlan>>(UiState.Loading)
    val plan: StateFlow<UiState<NodeResetPlan>> = _plan.asStateFlow()
    private val _state = MutableStateFlow<ResetState>(ResetState.Idle)
    val state: StateFlow<ResetState> = _state.asStateFlow()

    fun loadPlan() {
        _plan.value = UiState.Loading
        viewModelScope.launch { _plan.value = uiStateOf { talos.resetPlan(node) } }
    }

    fun run(request: ResetRequest) {
        if (_state.value is ResetState.Running) return
        _state.value = ResetState.Running
        viewModelScope.launch {
            _state.value = runCatching { talos.reset(node, request) }.fold(
                onSuccess = { ResetState.Done },
                onFailure = { ResetState.Failed(it.uiText()) },
            )
        }
    }

    fun dismiss() {
        _state.value = ResetState.Idle
    }
}

@get:StringRes
private val ResetWipe.label: Int
    get() = when (this) {
        ResetWipe.ALL -> R.string.reset_wipe_all
        ResetWipe.SYSTEM -> R.string.reset_wipe_system
        ResetWipe.USER -> R.string.reset_wipe_user
    }

/**
 * The plan (role, etcd member, blockers, warnings), what to wipe, `--graceful` and `--reboot`,
 * then the typed-hostname confirmation: one dialog, its button disabled while a blocker stands.
 * [initial]: the options preselected (a control plane already out of etcd resets without `--graceful`).
 */
@Composable
fun ResetConfirmDialog(
    vm: ResetViewModel,
    hostname: String,
    onConfirm: (ResetRequest) -> Unit,
    onDismiss: () -> Unit,
    initial: ResetRequest = ResetRequest(),
) {
    LaunchedEffect(Unit) { vm.loadPlan() }
    val planState by vm.plan.collectAsStateWithLifecycle()
    val plan = (planState as? UiState.Loaded)?.data
    var chosen by remember { mutableStateOf(initial) }
    // A mode the plan does not offer (no user disk) falls back to the system disk.
    val request = plan?.let { p -> chosen.copy(wipe = chosen.wipe.takeIf { it in p.wipeModes } ?: ResetWipe.SYSTEM) } ?: chosen

    HostnameConfirmDialog(
        title = stringResource(R.string.reset_confirm_title, hostname),
        hostname = hostname,
        confirmLabel = stringResource(R.string.reset_confirm),
        onConfirm = { onConfirm(request) },
        onDismiss = onDismiss,
        emphasized = true,
        enabled = plan?.allowed == true,
    ) {
        when (val s = planState) {
            UiState.Loading -> Row(verticalAlignment = Alignment.CenterVertically) {
                CircularProgressIndicator(Modifier.padding(end = 8.dp))
                Text(stringResource(R.string.reset_plan_loading), style = MaterialTheme.typography.bodyMedium)
            }
            is UiState.Failed -> Text(
                s.message.resolve(LocalContext.current),
                color = LocalStatusColors.current.bad,
                style = MaterialTheme.typography.bodyMedium,
            )
            is UiState.Loaded -> ResetPlanContent(s.data, request) { chosen = it }
        }
    }
}

@Composable
private fun ResetPlanContent(plan: NodeResetPlan, request: ResetRequest, onChange: (ResetRequest) -> Unit) {
    val colors = LocalStatusColors.current
    val wipe = request.wipe

    Text(
        stringResource(
            if (plan.role == "controlplane") R.string.reset_role_controlplane else R.string.reset_role_worker,
        ) + (plan.etcdMember?.let { " · " + stringResource(R.string.reset_etcd_member, it.id) } ?: ""),
        style = MaterialTheme.typography.bodyMedium,
    )
    plan.blockers.forEach { Text(it, color = colors.bad, style = MaterialTheme.typography.bodyMedium) }
    if (!plan.allowed) return
    plan.warnings.forEach { Text(it, color = colors.warn, style = MaterialTheme.typography.bodyMedium) }

    Column(Modifier.selectableGroup()) {
        Text(stringResource(R.string.reset_wipe_label), style = MaterialTheme.typography.labelLarge)
        plan.wipeModes.forEach { mode ->
            Row(
                Modifier.fillMaxWidth()
                    .selectable(selected = mode == wipe, role = Role.RadioButton, onClick = { onChange(request.copy(wipe = mode)) })
                    .padding(vertical = 4.dp),
                verticalAlignment = Alignment.Top,
            ) {
                RadioButton(selected = mode == wipe, onClick = null)
                Column(Modifier.padding(start = 8.dp)) {
                    Text(stringResource(mode.label), style = MaterialTheme.typography.bodyLarge)
                    if (mode != ResetWipe.SYSTEM) {
                        Text(
                            plan.userDisks.joinToString(", "),
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                }
            }
        }
    }
    ToggleRow(
        title = stringResource(R.string.reset_graceful),
        description = stringResource(R.string.reset_graceful_desc),
        checked = request.graceful,
        onChange = { onChange(request.copy(graceful = it)) },
    )
    if (plan.leavesDeadMember(request.graceful)) {
        Text(stringResource(R.string.reset_dead_member), color = colors.warn, style = MaterialTheme.typography.bodyMedium)
    }
    ToggleRow(
        title = stringResource(R.string.reset_reboot),
        description = stringResource(if (request.reboot) R.string.reset_reboot_desc else R.string.power_shutdown_stays_off),
        checked = request.reboot,
        onChange = { onChange(request.copy(reboot = it)) },
    )
}
