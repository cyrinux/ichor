package name.levis.ichor.ui.kubeevents

import androidx.lifecycle.ViewModel
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import name.levis.ichor.data.KubeEventsItem
import name.levis.ichor.data.KubeRepository
import name.levis.ichor.model.KubeEventsFeed
import name.levis.ichor.model.KubeEventsStatus
import name.levis.ichor.ui.userMessage

data class KubeEventsState(
    val feed: KubeEventsFeed = KubeEventsFeed(),
    /** Null until the stream says how it runs: connecting. */
    val status: KubeEventsStatus? = null,
    val streaming: Boolean = false,
    /** Why the stream ended; null while it runs or when it was cancelled. */
    val error: String? = null,
    /** The first batch has come: an empty list means no event, not still loading. */
    val received: Boolean = false,
)

/** What a stream was started for: another one starts from an empty list. */
private data class KubeEventsQuery(val namespace: String?, val warningsOnly: Boolean, val masked: Boolean)

/** The cluster's events kept live while [stream] is collected (the screen visible). */
class KubeEventsViewModel(private val kube: KubeRepository) : ViewModel() {
    private val _state = MutableStateFlow(KubeEventsState())
    val state: StateFlow<KubeEventsState> = _state.asStateFlow()
    private var query: KubeEventsQuery? = null

    /**
     * Runs until cancelled or the stream ends. Rows are kept across a restart with the same
     * filters (the Go core's first batch replaces them); other filters, or the privacy mask
     * turned on or off, start from an empty list, not paused.
     */
    suspend fun stream(namespace: String?, warningsOnly: Boolean, masked: Boolean) {
        val next = KubeEventsQuery(namespace, warningsOnly, masked)
        val same = next == query
        query = next
        _state.update { s ->
            if (same) s.copy(streaming = true, error = null, status = null) else KubeEventsState(streaming = true)
        }
        try {
            kube.eventsStream(namespace, warningsOnly).collect { item ->
                when (item) {
                    is KubeEventsItem.Batch -> _state.update { it.copy(feed = it.feed.receive(item.batch), received = true) }
                    is KubeEventsItem.Status -> _state.update { it.copy(status = item.status) }
                    is KubeEventsItem.Done -> _state.update { it.copy(streaming = false, error = item.error) }
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

    /** Stops applying batches; they wait, counted, until [resume]. */
    fun pause() = _state.update { it.copy(feed = it.feed.pause()) }

    fun resume() = _state.update { it.copy(feed = it.feed.resume()) }
}
