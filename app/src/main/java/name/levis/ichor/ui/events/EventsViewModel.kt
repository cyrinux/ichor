package name.levis.ichor.ui.events

import androidx.lifecycle.ViewModel
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import name.levis.ichor.data.StreamItem
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.model.TalosEvent
import name.levis.ichor.model.withEvent
import name.levis.ichor.ui.userMessage

/** Events replayed per node when the stream (re)starts, like `talosctl events --tail 50`. */
const val EVENTS_TAIL = 50

data class EventsState(
    /** Newest first, at most MAX_EVENTS. */
    val events: List<TalosEvent> = emptyList(),
    val streaming: Boolean = false,
    val error: String? = null,
)

/** Streams machine events from [node] (null: every node of the context) while collected. */
class EventsViewModel(private val talos: TalosRepository, private val node: String?) : ViewModel() {
    private val _state = MutableStateFlow(EventsState())
    val state: StateFlow<EventsState> = _state.asStateFlow()

    /** Runs until cancelled or the stream ends; events already received are kept across restarts. */
    suspend fun stream() {
        _state.update { it.copy(streaming = true, error = null) }
        try {
            talos.events(listOfNotNull(node), EVENTS_TAIL).collect { item ->
                when (item) {
                    is StreamItem.Item -> _state.update { it.copy(events = it.events.withEvent(item.value)) }
                    is StreamItem.Done -> _state.update { it.copy(streaming = false, error = item.error) }
                }
            }
        } catch (e: CancellationException) {
            throw e
        } catch (e: Throwable) {
            _state.update { it.copy(error = e.userMessage()) }
        } finally {
            _state.update { it.copy(streaming = false) }
        }
    }
}
