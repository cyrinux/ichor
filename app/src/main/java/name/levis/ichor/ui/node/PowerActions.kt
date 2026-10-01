package name.levis.ichor.ui.node

import androidx.annotation.StringRes
import androidx.compose.ui.res.stringResource
import name.levis.ichor.R
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.selection.selectableGroup
import androidx.compose.foundation.selection.toggleable
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.unit.dp
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.uiText
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch

enum class PowerAction(@StringRes val title: Int) {
    REBOOT(R.string.power_reboot),
    SHUTDOWN(R.string.power_shut_down),
}

/** `talosctl reboot -m`; descriptions follow the Talos v1.14 reboot sequence. */
enum class RebootMode(val cli: String, @StringRes val label: Int, @StringRes val description: Int) {
    DEFAULT("default", R.string.power_mode_graceful, R.string.power_mode_graceful_desc),
    POWERCYCLE("powercycle", R.string.power_power_cycle, R.string.power_mode_powercycle_desc),
    FORCE("force", R.string.power_mode_force, R.string.power_mode_force_desc),
}

data class PowerRequest(
    val action: PowerAction,
    val rebootMode: RebootMode = RebootMode.DEFAULT,
    /** `talosctl shutdown --force`: skip the Kubernetes cordon/drain. */
    val forceShutdown: Boolean = false,
) {
    val forced: Boolean
        get() = when (action) {
            PowerAction.REBOOT -> rebootMode == RebootMode.FORCE
            PowerAction.SHUTDOWN -> forceShutdown
        }

    /** Button / auth-prompt label, e.g. "Force reboot", "Power cycle", "Shut down". */
    val title: Int
        @StringRes get() = when {
            action == PowerAction.REBOOT && rebootMode == RebootMode.POWERCYCLE -> R.string.power_power_cycle
            forced && action == PowerAction.REBOOT -> R.string.power_force_reboot
            forced -> R.string.power_force_shut_down
            else -> action.title
        }
}

sealed interface PowerState {
    data object Idle : PowerState
    data class Running(val request: PowerRequest) : PowerState
    data class Done(val request: PowerRequest) : PowerState
    data class Failed(val message: UiText) : PowerState
}

class PowerViewModel(private val talos: TalosRepository, private val node: String) : ViewModel() {
    private val _state = MutableStateFlow<PowerState>(PowerState.Idle)
    val state: StateFlow<PowerState> = _state.asStateFlow()

    fun run(request: PowerRequest) {
        if (_state.value is PowerState.Running) return
        _state.value = PowerState.Running(request)
        viewModelScope.launch {
            _state.value = runCatching {
                when (request.action) {
                    PowerAction.REBOOT -> talos.reboot(node, request.rebootMode.cli)
                    PowerAction.SHUTDOWN -> talos.shutdown(node, request.forceShutdown)
                }
            }.fold(
                onSuccess = { PowerState.Done(request) },
                onFailure = { PowerState.Failed(it.uiText()) },
            )
        }
    }

    fun dismiss() {
        _state.value = PowerState.Idle
    }
}

/** Options (reboot mode / force shutdown) plus the typed-hostname confirmation. */
@Composable
fun PowerConfirmDialog(
    action: PowerAction,
    hostname: String,
    role: String,
    onConfirm: (PowerRequest) -> Unit,
    onDismiss: () -> Unit,
) {
    var mode by remember { mutableStateOf(RebootMode.DEFAULT) }
    var forceShutdown by remember { mutableStateOf(false) }
    val request = PowerRequest(action, mode, forceShutdown)
    val colors = LocalStatusColors.current

    HostnameConfirmDialog(
        title = stringResource(R.string.power_confirm_title, stringResource(action.title), hostname),
        hostname = hostname,
        confirmLabel = stringResource(request.title),
        onConfirm = { onConfirm(request) },
        onDismiss = onDismiss,
        emphasized = request.forced,
    ) {
        when (action) {
            PowerAction.REBOOT -> RebootModePicker(mode) { mode = it }
            PowerAction.SHUTDOWN -> ForceShutdownSwitch(forceShutdown) { forceShutdown = it }
        }
        if (role == "controlplane") {
            Text(
                stringResource(R.string.power_controlplane_warning),
                color = colors.warn,
                style = MaterialTheme.typography.bodyMedium,
            )
        }
        if (action == PowerAction.SHUTDOWN) {
            Text(
                stringResource(R.string.power_shutdown_stays_off),
                style = MaterialTheme.typography.bodyMedium,
            )
        }
    }
}

@Composable
private fun RebootModePicker(selected: RebootMode, onSelect: (RebootMode) -> Unit) {
    val colors = LocalStatusColors.current
    Column(Modifier.selectableGroup()) {
        Text(stringResource(R.string.power_mode_label), style = MaterialTheme.typography.labelLarge)
        RebootMode.entries.forEach { mode ->
            Row(
                Modifier.fillMaxWidth()
                    .selectable(selected = mode == selected, role = Role.RadioButton, onClick = { onSelect(mode) })
                    .padding(vertical = 4.dp),
                verticalAlignment = Alignment.Top,
            ) {
                RadioButton(selected = mode == selected, onClick = null)
                Column(Modifier.padding(start = 8.dp)) {
                    Text(stringResource(mode.label), style = MaterialTheme.typography.bodyLarge)
                    Text(
                        stringResource(mode.description),
                        style = MaterialTheme.typography.bodySmall,
                        color = if (mode == RebootMode.FORCE) colors.bad else MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
        }
    }
}

@Composable
private fun ForceShutdownSwitch(checked: Boolean, onChange: (Boolean) -> Unit) {
    Row(
        Modifier.fillMaxWidth().toggleable(value = checked, role = Role.Switch, onValueChange = onChange),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(Modifier.weight(1f)) {
            Text(stringResource(R.string.power_force_label), style = MaterialTheme.typography.bodyLarge)
            Text(
                stringResource(R.string.power_force_desc),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        Switch(checked = checked, onCheckedChange = null, modifier = Modifier.padding(start = 8.dp))
    }
}
