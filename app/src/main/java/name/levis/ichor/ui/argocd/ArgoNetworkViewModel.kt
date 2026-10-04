package name.levis.ichor.ui.argocd

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.receiveAsFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.model.ArgoApp
import name.levis.ichor.model.ArgoNetwork
import name.levis.ichor.model.KubePod
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.refreshFailed
import name.levis.ichor.ui.uiText
import name.levis.ichor.ui.workloads.DeleteResult

/**
 * The network view of one app detail: loaded when the section first shows, then again each
 * time the app itself reloads (pull to refresh, the polling while a sync runs). A reload never
 * interrupts one in flight, so a slow cluster still gets an answer between two polls.
 */
class ArgoNetworkViewModel(private val talos: TalosRepository) : ViewModel() {
    private val _state = MutableStateFlow<UiState<ArgoNetwork>>(UiState.Loading)
    val state: StateFlow<UiState<ArgoNetwork>> = _state.asStateFlow()
    private var job: Job? = null
    private var source: Any? = null

    private val _deleting = MutableStateFlow<Set<String>>(emptySet())
    /** Keys of the pods whose deletion is in flight. */
    val deleting: StateFlow<Set<String>> = _deleting.asStateFlow()

    private val _results = Channel<DeleteResult>(Channel.BUFFERED)
    val results: Flow<DeleteResult> = _results.receiveAsFlow()

    /** Loads [app]'s graph once per [key] (when the app was fetched); the data on screen stays meanwhile. */
    fun load(app: ArgoApp, key: Any) {
        if (key == source) return
        source = key
        if (job?.isActive == true) return
        fetch(app)
    }

    private fun fetch(app: ArgoApp) {
        job = viewModelScope.launch {
            _state.value = try {
                UiState.Loaded(talos.argoNetwork(app))
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                _state.value.refreshFailed(e.uiText())
            }
        }
    }

    /** `kubectl delete pod`: its controller starts a fresh one, which the next load shows. */
    fun deletePod(app: ArgoApp, namespace: String, name: String) {
        val pod = KubePod(namespace = namespace, name = name)
        if (pod.key in _deleting.value) return
        _deleting.update { it + pod.key }
        viewModelScope.launch {
            val outcome = runCatching { talos.deletePod(pod) }
            _deleting.update { it - pod.key }
            outcome.exceptionOrNull().takeIf { it is CancellationException }?.let { throw it }
            _results.send(DeleteResult(pod, outcome.exceptionOrNull()?.uiText()))
            if (outcome.isSuccess && job?.isActive != true) fetch(app)
        }
    }
}
