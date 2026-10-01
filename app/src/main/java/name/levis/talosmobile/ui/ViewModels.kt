package name.levis.talosmobile.ui

import androidx.lifecycle.ViewModel
import androidx.lifecycle.ViewModelProvider.AndroidViewModelFactory.Companion.APPLICATION_KEY
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.CreationExtras
import androidx.lifecycle.viewmodel.initializer
import androidx.lifecycle.viewmodel.viewModelFactory
import name.levis.talosmobile.TalosApp
import name.levis.talosmobile.data.TalosRepository
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
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

    protected abstract suspend fun fetch(): T

    /** Last cached value to show instantly while [fetch] runs (stale-while-revalidate). */
    protected open fun cached(): TalosRepository.Timed<T>? = null

    /** [reset] drops the current data first, e.g. when the data source (context) changed. */
    fun refresh(reset: Boolean = false) {
        job?.cancel()
        val previous = _state.value
        _state.value = when {
            !reset && previous is UiState.Loaded -> previous.copy(refreshing = true)
            else -> cached()?.let { UiState.Loaded(it.value, refreshing = true, fetchedAt = it.at) } ?: UiState.Loading
        }
        job = viewModelScope.launch {
            _state.value = try {
                UiState.Loaded(fetch())
            } catch (e: CancellationException) {
                throw e // superseded by a newer refresh: don't report it as a failure
            } catch (e: Throwable) {
                UiState.Failed(e.userMessage())
            }
        }
    }
}
