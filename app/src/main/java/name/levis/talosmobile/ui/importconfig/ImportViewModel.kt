package name.levis.talosmobile.ui.importconfig

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import name.levis.talosmobile.data.ConfigRepository
import name.levis.talosmobile.model.ConfigSummary
import name.levis.talosmobile.ui.userMessage
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch

sealed interface ImportState {
    data object Idle : ImportState
    data object Validating : ImportState
    data class Preview(val yaml: String, val summary: ConfigSummary) : ImportState
    data class Invalid(val message: String) : ImportState
    data object Saved : ImportState
}

class ImportViewModel(private val configs: ConfigRepository) : ViewModel() {
    private val _state = MutableStateFlow<ImportState>(ImportState.Idle)
    val state: StateFlow<ImportState> = _state.asStateFlow()

    /** Validates candidate YAML from any source (file, paste, QR) and shows a preview. */
    fun submit(yaml: String) {
        _state.value = ImportState.Validating
        viewModelScope.launch {
            _state.value = runCatching { configs.validate(yaml) }.fold(
                onSuccess = { ImportState.Preview(yaml, it) },
                onFailure = { ImportState.Invalid(it.userMessage()) },
            )
        }
    }

    fun confirm() {
        val preview = _state.value as? ImportState.Preview ?: return
        viewModelScope.launch {
            _state.value = runCatching { configs.save(preview.yaml) }.fold(
                onSuccess = { ImportState.Saved },
                onFailure = { ImportState.Invalid(it.userMessage()) },
            )
        }
    }

    fun reset() {
        _state.value = ImportState.Idle
    }
}
