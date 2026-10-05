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
import name.levis.ichor.data.ARGO_CD
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.model.ArgoAction
import name.levis.ichor.model.ArgoApp
import name.levis.ichor.model.ArgoResource
import name.levis.ichor.model.ArgoStatus
import name.levis.ichor.model.ArgoSyncOptions
import name.levis.ichor.model.KubeWorkload
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.refreshFailed
import name.levis.ichor.ui.uiText
import name.levis.ichor.ui.workloads.WorkloadRestarts

/** How an action on [count] apps ended: [failed] of them with [error] (the first one's), [app] when only one. */
data class ArgoActionResult(val action: ArgoAction, val count: Int, val app: String?, val failed: Int, val error: UiText?)

/**
 * Argo CD for the overview card, the apps screen, the app detail and the app sheet: loads on
 * demand from the cached result first, polls quietly (no refresh indicator) while a sync runs
 * or right after an action, and runs actions with their outcome as one-shot [results].
 */
class ArgoViewModel(private val talos: TalosRepository) : ViewModel() {
    private val _state = MutableStateFlow<UiState<ArgoStatus>>(UiState.Loading)
    val state: StateFlow<UiState<ArgoStatus>> = _state.asStateFlow()
    private var job: Job? = null
    private var source: Any? = null

    /** Polls until then (epoch millis) even with no sync seen yet: the controller takes a moment. */
    private var boostUntil = 0L

    private val _busy = MutableStateFlow<Set<String>>(emptySet())
    /** Keys of the apps whose action request is in flight. */
    val busy: StateFlow<Set<String>> = _busy.asStateFlow()

    // A queue, not a state: two actions finishing together each get their message.
    private val _results = Channel<ArgoActionResult>(Channel.BUFFERED)
    val results: Flow<ArgoActionResult> = _results.receiveAsFlow()

    /** Rollout restarts of the app's workloads, followed like an action. */
    val restarts = WorkloadRestarts(viewModelScope, talos) { boost() }

    /** Loads once per [key] (context, config generation, invalidations); [refresh] forces it. */
    fun load(key: Any) {
        if (key == source) return
        source = key
        fetch(quiet = false, reset = true)
    }

    /**
     * Like [load], but a result already fetched (by the overview or the Argo CD screen) is shown
     * as is, without asking the cluster again: for the app sheet, opened often.
     */
    fun loadOrReuse(key: Any) {
        if (key == source) return
        val cached = talos.cached<ArgoStatus>(ARGO_CD)
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

    /** Whether the screen should poll: a sync is running, or an action was just requested. */
    val shouldPoll: Boolean
        get() = System.currentTimeMillis() < boostUntil ||
            (state.value as? UiState.Loaded)?.data?.apps?.any { it.isRunning } == true

    private fun fetch(quiet: Boolean, reset: Boolean) {
        job?.cancel()
        val previous = _state.value
        if (!quiet) {
            _state.value = when {
                !reset && previous is UiState.Loaded -> previous.copy(refreshing = true)
                else -> talos.cached<ArgoStatus>(ARGO_CD)?.let { UiState.Loaded(it.value, refreshing = true, fetchedAt = it.at) } ?: UiState.Loading
            }
        }
        job = viewModelScope.launch {
            _state.value = try {
                UiState.Loaded(talos.argoCD())
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                _state.value.refreshFailed(e.uiText(), talos.cached<ArgoStatus>(ARGO_CD)?.let { it.value to it.at })
            }
        }
    }

    /** Runs [action] on every app of [apps], one after the other, then follows the outcome. */
    fun act(apps: List<ArgoApp>, action: ArgoAction, options: ArgoSyncOptions? = null) {
        val keys = apps.map { it.key }.toSet()
        if (keys.isEmpty() || keys.any { it in _busy.value }) return
        _busy.update { it + keys }
        viewModelScope.launch {
            val errors = apps.mapNotNull { app ->
                runCatching { talos.argoAction(app, action, options) }.exceptionOrNull()
                    ?.takeUnless { it is CancellationException }?.uiText()
            }
            _busy.update { it - keys }
            _results.send(ArgoActionResult(action, apps.size, apps.singleOrNull()?.name, errors.size, errors.firstOrNull()))
            if (errors.size < apps.size) boost()
        }
    }

    /**
     * The workload behind [resource] at once, from a Kubernetes list when one holds it (its
     * replicas matter to the confirmation); [WorkloadRestarts.current] reads it fresh.
     */
    fun workloadFor(resource: ArgoResource): KubeWorkload =
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
