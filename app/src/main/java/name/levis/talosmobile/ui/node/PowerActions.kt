package name.levis.talosmobile.ui.node

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.selection.selectableGroup
import androidx.compose.foundation.selection.toggleable
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import name.levis.talosmobile.data.TalosRepository
import name.levis.talosmobile.ui.theme.LocalStatusColors
import name.levis.talosmobile.ui.userMessage
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch

enum class PowerAction(val verb: String, val title: String) {
    REBOOT("reboot", "Reboot"),
    SHUTDOWN("shut down", "Shut down"),
}

/** `talosctl reboot -m`; descriptions follow the Talos v1.14 reboot sequence. */
enum class RebootMode(val cli: String, val label: String, val description: String) {
    DEFAULT("default", "Graceful", "Stop pods and services, then reboot (kexec fast reboot when available)."),
    POWERCYCLE("powercycle", "Power cycle", "Graceful stop, then a full firmware reboot instead of kexec."),
    FORCE(
        "force",
        "Force",
        "Reboot immediately: pods and services are NOT stopped. Only for a stuck node.",
    ),
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
    val title: String
        get() = when {
            action == PowerAction.REBOOT && rebootMode == RebootMode.POWERCYCLE -> "Power cycle"
            forced -> "Force ${action.title.lowercase()}"
            else -> action.title
        }
}

sealed interface PowerState {
    data object Idle : PowerState
    data class Running(val request: PowerRequest) : PowerState
    data class Done(val request: PowerRequest) : PowerState
    data class Failed(val message: String) : PowerState
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
                onFailure = { PowerState.Failed(it.userMessage()) },
            )
        }
    }

    fun dismiss() {
        _state.value = PowerState.Idle
    }
}

/**
 * Options (reboot mode / force shutdown) plus a confirmation that requires typing the
 * hostname, GitHub-style, to avoid accidental taps.
 */
@Composable
fun PowerConfirmDialog(
    action: PowerAction,
    hostname: String,
    role: String,
    onConfirm: (PowerRequest) -> Unit,
    onDismiss: () -> Unit,
) {
    var typed by remember { mutableStateOf("") }
    var mode by remember { mutableStateOf(RebootMode.DEFAULT) }
    var forceShutdown by remember { mutableStateOf(false) }
    val request = PowerRequest(action, mode, forceShutdown)
    val matches = typed.trim() == hostname
    val colors = LocalStatusColors.current

    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("${action.title} $hostname?") },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                when (action) {
                    PowerAction.REBOOT -> RebootModePicker(mode) { mode = it }
                    PowerAction.SHUTDOWN -> ForceShutdownSwitch(forceShutdown) { forceShutdown = it }
                }
                if (role == "controlplane") {
                    Text(
                        "Control-plane node: it leaves etcd while down. Make sure the other members are " +
                            "healthy, or the cluster can lose quorum.",
                        color = colors.warn,
                        style = MaterialTheme.typography.bodyMedium,
                    )
                }
                if (action == PowerAction.SHUTDOWN) {
                    Text(
                        "It stays off until someone powers it on (Wake-on-LAN, IPMI or physically).",
                        style = MaterialTheme.typography.bodyMedium,
                    )
                }
                Text("Type $hostname to confirm:", style = MaterialTheme.typography.bodyMedium)
                OutlinedTextField(
                    value = typed,
                    onValueChange = { typed = it },
                    singleLine = true,
                    textStyle = MaterialTheme.typography.bodyMedium.copy(fontFamily = FontFamily.Monospace),
                    modifier = Modifier.fillMaxWidth(),
                )
            }
        },
        confirmButton = {
            TextButton(onClick = { onConfirm(request) }, enabled = matches) {
                Text(
                    request.title,
                    color = if (matches) colors.bad else MaterialTheme.colorScheme.onSurfaceVariant,
                    fontWeight = if (request.forced) FontWeight.Bold else null,
                )
            }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("Cancel") } },
    )
}

@Composable
private fun RebootModePicker(selected: RebootMode, onSelect: (RebootMode) -> Unit) {
    val colors = LocalStatusColors.current
    Column(Modifier.selectableGroup()) {
        Text("Mode (talosctl reboot -m)", style = MaterialTheme.typography.labelLarge)
        RebootMode.entries.forEach { mode ->
            Row(
                Modifier.fillMaxWidth()
                    .selectable(selected = mode == selected, role = Role.RadioButton, onClick = { onSelect(mode) })
                    .padding(vertical = 4.dp),
                verticalAlignment = Alignment.Top,
            ) {
                RadioButton(selected = mode == selected, onClick = null)
                Column(Modifier.padding(start = 8.dp)) {
                    Text(mode.label, style = MaterialTheme.typography.bodyLarge)
                    Text(
                        mode.description,
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
            Text("Force (--force)", style = MaterialTheme.typography.bodyLarge)
            Text(
                "Skip the Kubernetes cordon/drain, e.g. when the Kubernetes API is down. Pods and services " +
                    "are still stopped.",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        Switch(checked = checked, onCheckedChange = null, modifier = Modifier.padding(start = 8.dp))
    }
}
