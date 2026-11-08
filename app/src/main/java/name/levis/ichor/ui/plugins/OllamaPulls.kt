package name.levis.ichor.ui.plugins

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.catch
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import name.levis.ichor.data.PluginRepository
import name.levis.ichor.model.OllamaPullEvent
import name.levis.ichor.model.OllamaPullState

/**
 * The Ollama plugin screen's model pull, one at a time, held by the screen's ViewModel so it
 * survives a rotation and goes on while the process lives (no foreground service: Android may
 * stop it once the app is in the background for long). [onPulled] reads the plugin again so
 * the new model shows.
 */
class OllamaPulls(
    private val scope: CoroutineScope,
    private val plugins: PluginRepository,
    private val onPulled: () -> Unit,
) {
    private val _pull = MutableStateFlow<OllamaPullState?>(null)
    val pull: StateFlow<OllamaPullState?> = _pull.asStateFlow()
    private var job: Job? = null

    /** Pulls [model] onto [namespace]/[server]; nothing while another pull runs. */
    fun start(namespace: String, server: String, model: String) {
        if (_pull.value?.running == true) return
        _pull.value = OllamaPullState(namespace, server, model)
        job = scope.launch {
            // A pull that cannot start (no cluster selected) ends the run with its message.
            plugins.ollamaPull(namespace, server, model).catch { e ->
                emit(OllamaPullEvent.Failed(e.message.orEmpty()))
            }.collect { event ->
                when (event) {
                    is OllamaPullEvent.Progress -> _pull.update { it?.copy(progress = event.progress) }
                    OllamaPullEvent.Done -> {
                        _pull.update { it?.copy(running = false, done = true) }
                        onPulled()
                    }
                    is OllamaPullEvent.Failed -> _pull.update { it?.copy(running = false, error = event.message) }
                }
            }
        }
    }

    /** Stops the download (the layers already there stay on the server) and closes the sheet. */
    fun cancel() {
        job?.cancel()
        job = null
        _pull.value = null
    }

    /** Closes the sheet of a pull that ended. */
    fun dismiss() {
        if (_pull.value?.running == true) return
        _pull.value = null
    }
}
