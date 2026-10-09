package name.levis.ichor.ui.flux

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import name.levis.ichor.data.GitOpsRepository
import name.levis.ichor.model.FluxDiff
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.refreshFailed
import name.levis.ichor.ui.uiText

/** Which GitOps tool's diff a screen shows: the Go core computes both the same way. */
enum class DiffTool { FLUX, ARGO }

/**
 * The diff of one Flux or Argo CD object: computed when the screen opens (once per [load] key:
 * the context and config generation), again on refresh. The cluster builds it in a few seconds
 * (Argo CD may take longer: it renders in its own pod first); the result on screen stays while
 * a refresh runs.
 */
class FluxDiffViewModel(private val gitOps: GitOpsRepository, private val tool: DiffTool = DiffTool.FLUX) : ViewModel() {
    private val _state = MutableStateFlow<UiState<FluxDiff>>(UiState.Loading)
    val state: StateFlow<UiState<FluxDiff>> = _state.asStateFlow()
    private var job: Job? = null
    private var source: Any? = null

    fun load(kind: String, namespace: String, name: String, key: Any) {
        if (key == source) return
        source = key
        fetch(kind, namespace, name)
    }

    fun refresh(kind: String, namespace: String, name: String) = fetch(kind, namespace, name)

    private fun fetch(kind: String, namespace: String, name: String) {
        job?.cancel()
        (_state.value as? UiState.Loaded)?.let { _state.value = it.copy(refreshing = true) }
        job = viewModelScope.launch {
            _state.value = try {
                UiState.Loaded(
                    when (tool) {
                        DiffTool.FLUX -> gitOps.fluxDiff(kind, namespace, name)
                        DiffTool.ARGO -> gitOps.argoDiff(namespace, name)
                    },
                )
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                _state.value.refreshFailed(e.uiText())
            }
        }
    }
}
