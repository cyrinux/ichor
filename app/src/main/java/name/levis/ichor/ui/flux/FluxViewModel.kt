package name.levis.ichor.ui.flux

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
import name.levis.ichor.data.FLUX
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.model.FluxAction
import name.levis.ichor.model.FluxResource
import name.levis.ichor.model.FluxStatus
import name.levis.ichor.model.KubeWorkload
import name.levis.ichor.model.anyBusy
import name.levis.ichor.model.fluxKey
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.refreshFailed
import name.levis.ichor.ui.uiText
import name.levis.ichor.ui.workloads.WorkloadRestarts

/** How an action on the Flux object [name] ended: [error] when refused. */
data class FluxActionResult(val action: FluxAction, val name: String, val error: UiText?)

/**
 * Flux for the overview card, its screen, an app's detail and the Flux tile's sheet: loads on
 * demand from the cached result first, polls quietly (no refresh indicator) while a controller
 * reconciles or right after an action, and runs actions with their outcome as one-shot [results].
 */
class FluxViewModel(private val talos: TalosRepository) : ViewModel() {
    private val _state = MutableStateFlow<UiState<FluxStatus>>(UiState.Loading)
    val state: StateFlow<UiState<FluxStatus>> = _state.asStateFlow()
    private var job: Job? = null
    private var source: Any? = null

    /** Polls until then (epoch millis) even with nothing seen reconciling yet: the controller takes a moment. */
    private var boostUntil = 0L

    private val _busy = MutableStateFlow<Set<String>>(emptySet())
    /** Keys ([fluxKey]) of the objects whose action request is in flight. */
    val busy: StateFlow<Set<String>> = _busy.asStateFlow()

    // A queue, not a state: two actions finishing together each get their message.
    private val _results = Channel<FluxActionResult>(Channel.BUFFERED)
    val results: Flow<FluxActionResult> = _results.receiveAsFlow()

    /** Rollout restarts of a Kustomization's workloads, followed like an action. */
    val restarts = WorkloadRestarts(viewModelScope, talos) { boost() }

    /** Loads once per [key] (context, config generation, invalidations); [refresh] forces it. */
    fun load(key: Any) {
        if (key == source) return
        source = key
        fetch(quiet = false, reset = true)
    }

    /** Like [load], but a result already fetched is shown as is: for the Flux tile's sheet, opened often. */
    fun loadOrReuse(key: Any) {
        if (key == source) return
        val cached = talos.cached<FluxStatus>(FLUX)
        if (cached == null) return load(key)
        source = key
        _state.value = UiState.Loaded(cached.value, fetchedAt = cached.at)
    }

    /** Nothing to load: the next [load], even with a key seen before, loads again. */
    fun forget() {
        source = null
    }

    fun refresh() = fetch(quiet = false, reset = false)

    /** A refresh without the indicator, skipped while one is running. */
    fun poll() {
        if (job?.isActive == true) return
        fetch(quiet = true, reset = false)
    }

    /** Whether the screen should poll: something reconciles or waits for it, or an action was just requested. */
    val shouldPoll: Boolean
        get() = System.currentTimeMillis() < boostUntil ||
            (state.value as? UiState.Loaded)?.data?.anyBusy == true

    private fun fetch(quiet: Boolean, reset: Boolean) {
        job?.cancel()
        val previous = _state.value
        if (!quiet) {
            _state.value = when {
                !reset && previous is UiState.Loaded -> previous.copy(refreshing = true)
                else -> talos.cached<FluxStatus>(FLUX)?.let { UiState.Loaded(it.value, refreshing = true, fetchedAt = it.at) } ?: UiState.Loading
            }
        }
        job = viewModelScope.launch {
            _state.value = try {
                UiState.Loaded(talos.flux())
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                _state.value.refreshFailed(e.uiText(), talos.cached<FluxStatus>(FLUX)?.let { it.value to it.at })
            }
        }
    }

    /** Runs [action] on the object [kind] [namespace]/[name], then follows the outcome. */
    fun act(kind: String, namespace: String, name: String, action: FluxAction) {
        val key = fluxKey(kind, namespace, name)
        if (key in _busy.value) return
        _busy.update { it + key }
        viewModelScope.launch {
            val error = runCatching { talos.fluxAction(kind, namespace, name, action) }.exceptionOrNull()
                ?.takeUnless { it is CancellationException }?.uiText()
            _busy.update { it - key }
            _results.send(FluxActionResult(action, name, error))
            if (error == null) boost()
        }
    }

    /**
     * The workload behind [resource] at once, from a Kubernetes list when one holds it (its
     * replicas matter to the confirmation); [WorkloadRestarts.current] reads it fresh.
     */
    fun workloadFor(resource: FluxResource): KubeWorkload =
        talos.cachedWorkload(resource.kind, resource.namespace, resource.name)
            ?: KubeWorkload(kind = resource.kind, namespace = resource.namespace, name = resource.name, desired = UNKNOWN_REPLICAS)

    private fun boost() {
        boostUntil = System.currentTimeMillis() + BOOST_MILLIS
        fetch(quiet = true, reset = false)
    }

    private companion object {
        const val BOOST_MILLIS = 12_000L

        /** Not known here: more than one, so the restart dialog does not warn about downtime. */
        const val UNKNOWN_REPLICAS = 2
    }
}
