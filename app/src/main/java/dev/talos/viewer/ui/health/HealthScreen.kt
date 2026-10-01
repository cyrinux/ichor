package dev.talos.viewer.ui.health

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.compose.viewModel
import androidx.compose.ui.platform.LocalContext
import dev.talos.viewer.TalosApp
import dev.talos.viewer.data.HealthEvent
import dev.talos.viewer.data.activeSummary
import dev.talos.viewer.model.Feature
import dev.talos.viewer.model.allows
import dev.talos.viewer.ui.components.RoleNotice
import dev.talos.viewer.data.TalosRepository
import dev.talos.viewer.ui.app
import dev.talos.viewer.ui.components.StatusPill
import dev.talos.viewer.ui.factory
import dev.talos.viewer.ui.theme.LocalStatusColors
import dev.talos.viewer.ui.userMessage
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.catch
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

data class HealthState(
    val running: Boolean = false,
    val lines: List<String> = emptyList(),
    val finished: Boolean = false,
    val error: String? = null,
)

class HealthViewModel(private val talos: TalosRepository) : ViewModel() {
    private val _state = MutableStateFlow(HealthState())
    val state: StateFlow<HealthState> = _state.asStateFlow()
    private var job: Job? = null

    fun start() {
        job?.cancel()
        _state.value = HealthState(running = true)
        job = viewModelScope.launch {
            talos.health()
                .catch { e -> _state.update { it.copy(running = false, finished = true, error = e.userMessage()) } }
                .collect { event ->
                    _state.update { s ->
                        when (event) {
                            is HealthEvent.Progress -> s.copy(lines = s.lines + event.message)
                            is HealthEvent.Done -> s.copy(running = false, finished = true, error = event.error)
                        }
                    }
                }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun HealthScreen(
    onBack: () -> Unit,
    vm: HealthViewModel = viewModel(factory = factory { HealthViewModel(app.talosRepository) }),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    val config by (LocalContext.current.applicationContext as TalosApp).configRepository.config.collectAsStateWithLifecycle()
    val summary = config?.activeSummary
    val allowed = summary?.allows(Feature.HEALTH) ?: true
    LaunchedEffect(allowed) { if (allowed && !state.running && !state.finished) vm.start() }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text("Cluster health") },
                navigationIcon = { IconButton(onClick = onBack) { Icon(Icons.AutoMirrored.Filled.ArrowBack, "Back") } },
            )
        },
    ) { padding ->
        Column(Modifier.padding(padding).fillMaxSize().padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            if (!allowed) {
                RoleNotice(Feature.HEALTH, summary?.roles.orEmpty())
                Text(
                    "The overview and etcd screens show node readiness and etcd status with any role.",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                return@Column
            }
            HealthHeader(state, onRerun = vm::start)
            Card(Modifier.fillMaxWidth().weight(1f)) {
                LazyColumn(contentPadding = PaddingValues(12.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                    itemsIndexed(state.lines) { _, line ->
                        Text(line, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace)
                    }
                }
            }
        }
    }
}

@Composable
private fun HealthHeader(state: HealthState, onRerun: () -> Unit) {
    val colors = LocalStatusColors.current
    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
        when {
            state.running -> {
                CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp)
                Text("Running server-side checks…", Modifier.weight(1f))
            }
            state.error == null && state.finished -> {
                StatusPill("Healthy", colors.ok)
                Text("All checks passed", Modifier.weight(1f))
            }
            else -> {
                StatusPill("Unhealthy", colors.bad)
                Text("", Modifier.weight(1f))
            }
        }
        if (!state.running) Button(onClick = onRerun) { Text("Re-run") }
    }
    state.error?.let { Text(it, color = colors.bad, style = MaterialTheme.typography.bodyMedium) }
}
