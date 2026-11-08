package name.levis.ichor.ui.rotateca

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import name.levis.ichor.data.CARotationManager
import name.levis.ichor.data.CARotationRunState
import name.levis.ichor.data.NodeLifecycleRepository
import name.levis.ichor.model.CARotationPlan
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.uiStateOf
import name.levis.ichor.ui.uiText

/**
 * Rotating the active context's Talos API CA: the plan (read again on each refresh), then the
 * run, which [CARotationManager] follows app-wide so leaving the screen keeps it going.
 */
class RotateCAViewModel(
    private val lifecycle: NodeLifecycleRepository,
    private val manager: CARotationManager,
) : ViewModel() {
    private val _plan = MutableStateFlow<UiState<CARotationPlan>>(UiState.Loading)
    val plan: StateFlow<UiState<CARotationPlan>> = _plan.asStateFlow()

    /** Why the run could not start (it never reached the cluster). */
    private val _startError = MutableStateFlow<UiText?>(null)
    val startError: StateFlow<UiText?> = _startError.asStateFlow()

    val run: StateFlow<CARotationRunState?> = manager.current

    fun refresh() {
        val previous = (_plan.value as? UiState.Loaded)?.data
        _plan.value = previous?.let { UiState.Loaded(it, refreshing = true) } ?: UiState.Loading
        viewModelScope.launch { _plan.value = uiStateOf { lifecycle.caRotationPlan() } }
    }

    /** Starts the rotation (the user typed the context's name); Go checks the plan again first. */
    fun start() {
        _startError.value = null
        try {
            manager.start()
        } catch (e: Exception) {
            _startError.value = e.uiText()
        }
    }

    fun failStart(message: UiText) {
        _startError.value = message
    }

    fun stop() = manager.stop()

    /** Forgets the finished run and reads the plan again (the new CA's). */
    fun dismiss() {
        manager.dismiss()
        refresh()
    }
}
