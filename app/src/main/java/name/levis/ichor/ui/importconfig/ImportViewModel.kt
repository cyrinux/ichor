package name.levis.ichor.ui.importconfig

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import name.levis.ichor.data.ConfigRepository
import name.levis.ichor.model.ConfigSummary
import name.levis.ichor.model.ImportChoice
import name.levis.ichor.model.ImportConflict
import name.levis.ichor.model.takenNameChoices
import name.levis.ichor.ui.userMessage
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch

sealed interface ImportState {
    data object Idle : ImportState
    data object Validating : ImportState

    /**
     * A valid config, with its contexts named like stored ones ([conflicts]) and what the
     * user picked for each ([choices], one per conflict). [error] is a failed save, kept on
     * the preview so the choices can be fixed.
     */
    data class Preview(
        val yaml: String,
        val summary: ConfigSummary,
        val conflicts: List<ImportConflict> = emptyList(),
        val choices: List<ImportChoice> = conflicts.map { ImportChoice(it.index) },
        val takenNames: Set<Int> = emptySet(),
        val error: String? = null,
    ) : ImportState {
        val canImport: Boolean get() = takenNames.isEmpty()
    }

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
            _state.value = runCatching {
                ImportState.Preview(yaml, configs.validate(yaml), configs.importConflicts(yaml))
            }.getOrElse { ImportState.Invalid(it.userMessage()) }
        }
    }

    /** Stores the conflict at [index] under [name] (blank: the suggested name). */
    fun rename(index: Int, name: String) = updateChoice(index) { it.copy(name = name) }

    /** Replaces the stored context of the same cluster with the conflict at [index], or not. */
    fun setReplace(index: Int, replace: Boolean) = updateChoice(index) { it.copy(replace = replace) }

    private fun updateChoice(index: Int, change: (ImportChoice) -> ImportChoice) {
        val preview = _state.value as? ImportState.Preview ?: return
        val choices = preview.choices.map { if (it.index == index) change(it) else it }
        _state.value = preview.copy(choices = choices, takenNames = takenNames(preview, choices), error = null)
    }

    private fun takenNames(preview: ImportState.Preview, choices: List<ImportChoice>): Set<Int> {
        val stored = configs.config.value?.summary?.contexts.orEmpty().map { it.name }.toSet()
        return takenNameChoices(preview.conflicts, choices, stored, preview.summary.contexts.map { it.name })
    }

    fun confirm() {
        val preview = _state.value as? ImportState.Preview ?: return
        if (!preview.canImport) return
        _state.value = ImportState.Validating
        viewModelScope.launch {
            _state.value = runCatching { configs.save(preview.yaml, preview.choices) }.fold(
                onSuccess = { ImportState.Saved },
                onFailure = { preview.copy(error = it.userMessage()) },
            )
        }
    }

    fun reset() {
        _state.value = ImportState.Idle
    }

    fun startDemo() {
        if (_state.value is ImportState.Validating || _state.value is ImportState.Saved) return
        _state.value = ImportState.Validating
        viewModelScope.launch {
            _state.value = runCatching { configs.saveDemo() }.fold(
                onSuccess = { ImportState.Saved },
                onFailure = { ImportState.Invalid(it.userMessage()) },
            )
        }
    }
}
