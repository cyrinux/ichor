package name.levis.ichor.ui.node

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Warning
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.model.ContainerInfo
import name.levis.ichor.model.isCriticalService
import name.levis.ichor.model.system
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.uiText

/** The name a container goes by in dialogs and messages. */
val ContainerInfo.displayName: String get() = name.ifEmpty { id.take(12) }

sealed interface ContainerRestartState {
    data object Idle : ContainerRestartState
    data class Running(val container: ContainerInfo) : ContainerRestartState
    data class Done(val container: ContainerInfo) : ContainerRestartState
    data class Failed(val container: ContainerInfo, val message: UiText) : ContainerRestartState
}

/** Restarts one container of the node (`talosctl restart`); the Pods tab's polling shows it back. */
class ContainerRestartViewModel(private val talos: TalosRepository, private val node: String) : ViewModel() {
    private val _state = MutableStateFlow<ContainerRestartState>(ContainerRestartState.Idle)
    val state: StateFlow<ContainerRestartState> = _state.asStateFlow()

    fun run(container: ContainerInfo) {
        if (_state.value is ContainerRestartState.Running) return
        _state.value = ContainerRestartState.Running(container)
        viewModelScope.launch {
            // TalosRepository.call runs the blocking Go call on Dispatchers.IO.
            _state.value = runCatching { talos.containerRestart(node, container.namespace, container.id) }.fold(
                onSuccess = { ContainerRestartState.Done(container) },
                onFailure = { ContainerRestartState.Failed(container, it.uiText()) },
            )
        }
    }

    fun dismiss() {
        _state.value = ContainerRestartState.Idle
    }
}

/**
 * Confirms a container restart: a Talos system container (apid, trustd, an extension) asks
 * for the typed hostname, a Kubernetes one a plain confirm (the kubelet starts it again).
 */
@Composable
fun ContainerRestartDialog(container: ContainerInfo, hostname: String, onConfirm: () -> Unit, onDismiss: () -> Unit) {
    val title = stringResource(R.string.container_restart_confirm, container.displayName, hostname)
    val confirmLabel = stringResource(R.string.service_action_restart)
    if (container.system) {
        HostnameConfirmDialog(title = title, hostname = hostname, confirmLabel = confirmLabel, onConfirm = onConfirm, onDismiss = onDismiss) {
            Text(stringResource(R.string.container_restart_system_note), style = MaterialTheme.typography.bodyMedium)
            if (isCriticalService(container.name)) CriticalWarning(container.name)
        }
        return
    }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(title) },
        text = { Text(stringResource(R.string.container_restart_kubernetes_note), style = MaterialTheme.typography.bodyMedium) },
        confirmButton = { TextButton(onClick = onConfirm) { Text(confirmLabel, color = LocalStatusColors.current.bad) } },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}

@Composable
private fun CriticalWarning(name: String) {
    val warn = LocalStatusColors.current.warn
    Row(verticalAlignment = Alignment.Top, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        Icon(Icons.Outlined.Warning, contentDescription = null, tint = warn, modifier = Modifier.size(20.dp))
        Column(Modifier.padding(end = 4.dp)) {
            Text(stringResource(R.string.service_critical_warning, name), color = warn, style = MaterialTheme.typography.bodyMedium)
        }
    }
}
