package dev.talos.viewer.ui

import androidx.lifecycle.ViewModel
import androidx.lifecycle.ViewModelProvider.AndroidViewModelFactory.Companion.APPLICATION_KEY
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.CreationExtras
import androidx.lifecycle.viewmodel.initializer
import androidx.lifecycle.viewmodel.viewModelFactory
import dev.talos.viewer.TalosApp
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

    fun refresh() {
        job?.cancel()
        val previous = _state.value
        _state.value = if (previous is UiState.Loaded) previous.copy(refreshing = true) else UiState.Loading
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
