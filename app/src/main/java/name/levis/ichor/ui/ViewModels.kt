package name.levis.ichor.ui

import androidx.lifecycle.ViewModel
import androidx.lifecycle.ViewModelProvider.AndroidViewModelFactory.Companion.APPLICATION_KEY
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.CreationExtras
import androidx.lifecycle.viewmodel.initializer
import androidx.lifecycle.viewmodel.viewModelFactory
import name.levis.ichor.TalosApp
import name.levis.ichor.data.TalosRepository
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch

val CreationExtras.app: TalosApp get() = this[APPLICATION_KEY] as TalosApp

inline fun <reified VM : ViewModel> factory(crossinline create: CreationExtras.() -> VM) =
    viewModelFactory { initializer { create() } }

/** A ViewModel that loads one value and supports refresh while keeping stale data visible. */
abstract class LoadingViewModel<T> : ViewModel() {
    private val _state = MutableStateFlow<UiState<T>>(UiState.Loading)
    val state: StateFlow<UiState<T>> = _state.asStateFlow()
    private var job: Job? = null

    private val _settled = MutableStateFlow(0)
    /** Bumped each time a [fetch] ended, well or not: what follows the data live starts over then. */
    protected val settled: StateFlow<Int> = _settled.asStateFlow()

    protected abstract suspend fun fetch(): T

    /** Last cached value to show instantly while [fetch] runs (stale-while-revalidate). */
    protected open fun cached(): TalosRepository.Timed<T>? = null

    /**
     * Whether a failed refresh leaves the data on screen, marked with the error, instead of
     * an error box. Only where the screen shows [name.levis.ichor.ui.components.DataFreshness],
     * which tells the user the data could not be refreshed.
     */
    protected open val keepsDataOnFailure: Boolean = false

    /**
     * Emits when [cached] may have something new: the last known state was read from disk,
     * which can finish after a refresh started with nothing to show.
     */
    protected open val restores: Flow<*>? = null
    private var watch: Job? = null

    /** Whether the data on screen is a part [showPartial] put there, not a finished [fetch]. */
    private var partial = false

    /** [reset] drops the current data first, e.g. when the data source (context) changed. */
    fun refresh(reset: Boolean = false) {
        job?.cancel()
        watch?.cancel()
        partial = false
        val previous = _state.value
        _state.value = when {
            !reset && previous is UiState.Loaded -> previous.copy(refreshing = true)
            else -> cached()?.let { UiState.Loaded(it.value, refreshing = true, fetchedAt = it.at) } ?: UiState.Loading
        }
        if (_state.value !is UiState.Loaded) watch = watchRestores()
        job = viewModelScope.launch {
            _state.value = try {
                UiState.Loaded(fetch()).also { partial = false }
            } catch (e: CancellationException) {
                throw e // superseded by a newer refresh: don't report it as a failure
            } catch (e: Throwable) {
                if (keepsDataOnFailure) {
                    // Keep what is on screen; the last known value may have been restored meanwhile.
                    _state.value.refreshFailed(e.uiText(), cached()?.let { it.value to it.at })
                } else {
                    UiState.Failed(e.uiText())
                }
            }
            _settled.value++
        }
    }

    /**
     * For a [fetch] that arrives in parts (a list loaded page by page): shows [value] while
     * nothing else is on screen. Data already there (the previous load, the last known one)
     * stays until [fetch] returns (L12: a refresh keeps the old rows).
     */
    protected fun showPartial(value: T) {
        val current = _state.value
        if (current is UiState.Loading || partial && current is UiState.Loaded) {
            _state.value = UiState.Loaded(value, refreshing = true)
            partial = true
        }
    }

    /**
     * Replaces the data on screen with [transform] of it (a change seen live), fresh as of now;
     * nothing while none is on screen. A refresh in flight replaces it when it completes.
     */
    protected fun updateLoaded(transform: (T) -> T) {
        val current = _state.value as? UiState.Loaded ?: return
        _state.value = current.copy(data = transform(current.data), fetchedAt = System.currentTimeMillis(), error = null)
    }

    /**
     * Replaces the data on screen, [expected], with [value] (a page loaded on scroll), or
     * marks it with [error]; nothing, and false, when a refresh replaced it meanwhile.
     */
    protected fun replaceLoaded(expected: T, value: T, error: UiText? = null): Boolean {
        val current = _state.value
        if (current !is UiState.Loaded || current.data !== expected || current.refreshing) return false
        _state.value = current.copy(data = value, error = error)
        return true
    }

    /** Until something is on screen, shows the last known value as soon as it was read from disk. */
    private fun watchRestores(): Job? = restores?.let { flow ->
        viewModelScope.launch {
            flow.collect {
                val restored = cached() ?: return@collect
                _state.value = _state.value.orRestored(restored.value to restored.at, keepsDataOnFailure)
            }
        }
    }
}
