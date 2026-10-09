package name.levis.ichor.ui.kubebrowser

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.receiveAsFlow
import kotlinx.coroutines.launch
import name.levis.ichor.data.KubeBrowserRepository
import name.levis.ichor.model.ConfigKey
import name.levis.ichor.model.KubeConfigData
import name.levis.ichor.model.KubeObjectRef
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.cancellableCatching
import name.levis.ichor.ui.uiText

/**
 * The data tab of a Secret or ConfigMap ([ref]): its keys (sizes, hints, certificates) and the
 * pods using it. A Secret's value is read one key at a time ([reveal], after the app lock) and
 * only one stays [revealed]; nothing is cached or logged.
 */
class KubeConfigDataViewModel(private val browser: KubeBrowserRepository, val ref: KubeObjectRef) : ViewModel() {
    private val _data = MutableStateFlow<UiState<KubeConfigData>>(UiState.Loading)
    val data: StateFlow<UiState<KubeConfigData>> = _data.asStateFlow()

    private val _revealed = MutableStateFlow<ConfigKey?>(null)
    /** The one Secret key whose value is shown, null when none is. */
    val revealed: StateFlow<ConfigKey?> = _revealed.asStateFlow()

    private val _revealing = MutableStateFlow<String?>(null)
    /** The key being read, to show progress on its row. */
    val revealing: StateFlow<String?> = _revealing.asStateFlow()

    private val _messages = Channel<UiText>(Channel.BUFFERED)
    /** Why a value could not be read (screenshot mode, RBAC), for a snackbar. */
    val messages: Flow<UiText> = _messages.receiveAsFlow()

    private var load: Job? = null
    private var reveal: Job? = null

    /** Reads the keys again; a value shown leaves the screen. */
    fun refresh() {
        hide()
        load?.cancel()
        val previous = _data.value
        _data.value = if (previous is UiState.Loaded) previous.copy(refreshing = true) else UiState.Loading
        load = viewModelScope.launch {
            _data.value = cancellableCatching { browser.configData(ref) }
                .fold(onSuccess = { UiState.Loaded(it) }, onFailure = { UiState.Failed(it.uiText()) })
        }
    }

    /** Reads the value of the Secret's [key] (the caller asked the app lock first). */
    fun reveal(key: String) {
        reveal?.cancel()
        _revealed.value = null
        _revealing.value = key
        reveal = viewModelScope.launch {
            val outcome = cancellableCatching { browser.configData(ref, key) }
            _revealing.value = null
            outcome.fold(
                onSuccess = { d -> _revealed.value = d.keys.firstOrNull { it.key == key && it.revealed } },
                onFailure = { _messages.send(it.uiText()) },
            )
        }
    }

    /** Hides the value shown, dropping it from memory. */
    fun hide() {
        reveal?.cancel()
        _revealing.value = null
        _revealed.value = null
    }

    override fun onCleared() {
        _revealed.value = null
    }
}
