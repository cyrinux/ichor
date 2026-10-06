package name.levis.ichor.ui

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.model.KubeWorkload

/**
 * A status (Argo CD, Flux) shared by the overview card, its screens and the app sheet: loads on
 * demand from the result cached under [cacheKey] first, and polls quietly (no refresh
 * indicator) while [isBusy] or right after an action ([boost]).
 */
abstract class PolledStatusViewModel<T>(protected val talos: TalosRepository, private val cacheKey: String) : ViewModel() {
    private val _state = MutableStateFlow<UiState<T>>(UiState.Loading)
    val state: StateFlow<UiState<T>> = _state.asStateFlow()
    private var job: Job? = null
    private var source: Any? = null

    /** Polls until then (epoch millis) even with nothing seen busy yet: the controller takes a moment. */
    private var boostUntil = 0L

    /** Called as a fetch starts; what it returns loads the status. */
    protected abstract fun fetcher(): suspend () -> T

    /** Whether [data] shows something in progress, worth polling for. */
    protected abstract fun isBusy(data: T): Boolean

    /** A [load] with a new key starts: another cluster, or a new configuration. */
    protected open fun onNewSource() {}

    /** Loads once per [key] (context, config generation, invalidations); [refresh] forces it. */
    fun load(key: Any) {
        if (key == source) return
        source = key
        onNewSource()
        fetch(quiet = false, reset = true)
    }

    /** Like [load], but a result already fetched is shown as is, without asking the cluster again: for a sheet opened often. */
    fun loadOrReuse(key: Any) {
        if (key == source) return
        val cached = talos.cached<T>(cacheKey)
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

    /** Whether the screen should poll: something is in progress, or an action was just requested. */
    val shouldPoll: Boolean
        get() = System.currentTimeMillis() < boostUntil ||
            (state.value as? UiState.Loaded)?.data?.let(::isBusy) == true

    private fun fetch(quiet: Boolean, reset: Boolean) {
        job?.cancel()
        val previous = _state.value
        if (!quiet) {
            _state.value = when {
                !reset && previous is UiState.Loaded -> previous.copy(refreshing = true)
                else -> talos.cached<T>(cacheKey)?.let { UiState.Loaded(it.value, refreshing = true, fetchedAt = it.at) } ?: UiState.Loading
            }
        }
        val fetchStatus = fetcher()
        job = viewModelScope.launch {
            _state.value = try {
                UiState.Loaded(fetchStatus())
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                _state.value.refreshFailed(e.uiText(), talos.cached<T>(cacheKey)?.let { it.value to it.at })
            }
        }
    }

    /** Follows the outcome of an action just requested. */
    protected fun boost() {
        boostUntil = System.currentTimeMillis() + BOOST_MILLIS
        fetch(quiet = true, reset = false)
    }

    /**
     * The workload [kind] [namespace]/[name] at once, from a Kubernetes list when one holds it
     * (its replicas matter to the restart confirmation).
     */
    protected fun workloadOf(kind: String, namespace: String, name: String): KubeWorkload =
        talos.cachedWorkload(kind, namespace, name)
            ?: KubeWorkload(kind = kind, namespace = namespace, name = name, desired = UNKNOWN_REPLICAS)

    private companion object {
        const val BOOST_MILLIS = 12_000L

        /** Not known here: more than one, so the restart dialog does not warn about downtime. */
        const val UNKNOWN_REPLICAS = 2
    }
}
