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
import name.levis.ichor.model.ServiceAction
import name.levis.ichor.model.isCriticalService
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.uiText

/** A start/stop/restart of one service. */
data class ServiceRequest(val service: String, val action: ServiceAction)

sealed interface ServiceControlState {
    data object Idle : ServiceControlState
    data class Running(val request: ServiceRequest) : ServiceControlState
    data class Done(val request: ServiceRequest) : ServiceControlState
    data class Failed(val request: ServiceRequest, val message: UiText) : ServiceControlState
}

class ServiceControlViewModel(private val talos: TalosRepository, private val node: String) : ViewModel() {
    private val _state = MutableStateFlow<ServiceControlState>(ServiceControlState.Idle)
    val state: StateFlow<ServiceControlState> = _state.asStateFlow()

    fun run(request: ServiceRequest) {
        if (_state.value is ServiceControlState.Running) return
        _state.value = ServiceControlState.Running(request)
        viewModelScope.launch {
            // TalosRepository.call runs the blocking Go call on Dispatchers.IO.
            _state.value = runCatching { talos.serviceAction(node, request.service, request.action) }.fold(
                onSuccess = { ServiceControlState.Done(request) },
                onFailure = { ServiceControlState.Failed(request, it.uiText()) },
            )
        }
    }

    fun dismiss() {
        _state.value = ServiceControlState.Idle
    }
}

@Composable
fun ServiceConfirmDialog(
    request: ServiceRequest,
    hostname: String,
    onConfirm: () -> Unit,
    onDismiss: () -> Unit,
) {
    val colors = LocalStatusColors.current
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(request.action.confirmTitle, request.service, hostname)) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                if (isCriticalService(request.service) && request.action != ServiceAction.START) {
                    Row(verticalAlignment = Alignment.Top) {
                        Icon(Icons.Outlined.Warning, contentDescription = null, tint = colors.warn, modifier = Modifier.size(20.dp))
                        Text(
                            stringResource(R.string.service_critical_warning, request.service),
                            color = colors.warn,
                            style = MaterialTheme.typography.bodyMedium,
                            modifier = Modifier.padding(start = 8.dp),
                        )
                    }
                }
            }
        },
        confirmButton = {
            TextButton(onClick = onConfirm) {
                Text(
                    stringResource(request.action.label),
                    color = if (request.action == ServiceAction.START) MaterialTheme.colorScheme.primary else colors.bad,
                )
            }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}
