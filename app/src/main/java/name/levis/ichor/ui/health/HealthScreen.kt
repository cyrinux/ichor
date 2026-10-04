package name.levis.ichor.ui.health

import androidx.compose.ui.res.stringResource
import name.levis.ichor.R
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
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
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
import name.levis.ichor.TalosApp
import androidx.compose.material3.OutlinedButton
import name.levis.ichor.data.HealthEvent
import name.levis.ichor.data.activeSummary
import name.levis.ichor.data.healthCheckNote
import name.levis.ichor.model.Feature
import name.levis.ichor.model.allows
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.RoleNotice
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.userMessage
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
    onDiagnose: (note: String) -> Unit,
    vm: HealthViewModel = viewModel(factory = factory { HealthViewModel(app.talosRepository) }),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    val app = LocalContext.current.applicationContext as TalosApp
    val config by app.configRepository.config.collectAsStateWithLifecycle()
    val ai by app.aiPreferences.settings.collectAsStateWithLifecycle()
    val summary = config?.activeSummary
    val allowed = summary?.allows(Feature.HEALTH) ?: true
    LaunchedEffect(allowed) { if (allowed && !state.running && !state.finished) vm.start() }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text(stringResource(R.string.health_title)) },
                navigationIcon = { BackButton(onBack) },
            )
        },
    ) { padding ->
        Column(Modifier.padding(padding).fillMaxSize().padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            if (!allowed) {
                RoleNotice(Feature.HEALTH, summary.roles)
                MutedText(stringResource(R.string.health_any_role_hint))
                return@Column
            }
            HealthHeader(state, onRerun = vm::start)
            // Only when the optional AI diagnosis is on, and there is a failure to explain.
            state.error?.takeIf { ai.enabled }?.let { error ->
                OutlinedButton(onClick = { onDiagnose(healthCheckNote(error)) }, modifier = Modifier.fillMaxWidth()) {
                    Text(stringResource(R.string.ai_health_diagnose))
                }
            }
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
                Text(stringResource(R.string.health_running), Modifier.weight(1f))
            }
            state.error == null && state.finished -> {
                StatusPill(stringResource(R.string.common_status_healthy), colors.ok)
                Text(stringResource(R.string.health_all_passed), Modifier.weight(1f))
            }
            else -> {
                StatusPill(stringResource(R.string.common_status_unhealthy), colors.bad)
                Text("", Modifier.weight(1f))
            }
        }
        if (!state.running) Button(onClick = onRerun) { Text(stringResource(R.string.health_rerun)) }
    }
    state.error?.let { Text(it, color = colors.bad, style = MaterialTheme.typography.bodyMedium) }
}
