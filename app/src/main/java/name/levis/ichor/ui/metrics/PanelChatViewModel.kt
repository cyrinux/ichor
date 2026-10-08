package name.levis.ichor.ui.metrics

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.catch
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import name.levis.ichor.data.AiPreferences
import name.levis.ichor.data.ChatEvent
import name.levis.ichor.data.MetricsChatRepository
import name.levis.ichor.model.PanelSuggestion
import name.levis.ichor.model.PromPanel
import name.levis.ichor.model.PromSource
import name.levis.ichor.ui.userMessage
import name.levis.ichorgo.PromChat

/** One bubble: the user's message, or the model's answer with the panel it proposed. */
data class ChatMessage(
    val id: Long,
    val fromUser: Boolean,
    val text: String,
    val panel: PanelSuggestion? = null,
    /** Why the answer stopped early; [text] may still hold the part received. */
    val error: String? = null,
)

data class PanelChatState(
    val messages: List<ChatMessage> = emptyList(),
    val asking: Boolean = false,
    /** The conversation is open: the source's metric names were looked up (or given up on). */
    val ready: Boolean = false,
    val namesFailed: Boolean = false,
    /** Why the conversation could not be opened. */
    val error: String? = null,
)

/**
 * The panel assistant of one Metrics screen: a conversation about the cluster's source,
 * started again when the source or the panel being edited changes. The metric names are
 * read once per source, best effort: without them the model uses the usual names.
 */
class PanelChatViewModel(
    private val repository: MetricsChatRepository,
    private val preferences: AiPreferences,
) : ViewModel() {
    private val _state = MutableStateFlow(PanelChatState())
    val state: StateFlow<PanelChatState> = _state.asStateFlow()

    private var chat: PromChat? = null
    private var openedFor: Pair<PromSource, PromPanel?>? = null
    private var names: Pair<PromSource, List<String>>? = null
    private var openJob: Job? = null
    private var askJob: Job? = null
    private var nextId = 0L

    /** Opens the conversation for [source] and [current]; the same pair keeps the one on screen. */
    fun open(source: PromSource, current: PromPanel?) {
        val key = source to current
        if (openedFor == key && (chat != null || openJob?.isActive == true)) return
        openedFor = key
        stop()
        openJob?.cancel()
        chat = null
        _state.value = PanelChatState()
        openJob = viewModelScope.launch {
            var failed = false
            val known = names?.takeIf { it.first == source }?.second ?: try {
                repository.metricNames(source).also { names = source to it }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                failed = true
                emptyList()
            }
            try {
                chat = repository.newChat(source, known, current)
                _state.update { it.copy(ready = true, namesFailed = failed) }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                _state.update { it.copy(error = e.userMessage()) }
            }
        }
    }

    fun send(text: String, language: String) {
        val chat = chat ?: return
        val message = text.trim()
        if (message.isEmpty() || _state.value.asking) return
        val settings = preferences.settings.value
        val answerId = nextId + 1
        _state.update {
            it.copy(
                messages = it.messages + ChatMessage(nextId, fromUser = true, text = message) + ChatMessage(answerId, fromUser = false, text = ""),
                asking = true,
            )
        }
        nextId += 2
        askJob = viewModelScope.launch {
            repository.ask(chat, settings, preferences.apiKey(settings.provider), language, message)
                .catch { e -> finish(answerId, null, e.userMessage()) }
                .collect { event ->
                    when (event) {
                        is ChatEvent.Text -> updateAnswer(answerId) { it.copy(text = event.text) }
                        is ChatEvent.Done -> finish(answerId, event.panel, event.error)
                    }
                }
        }
    }

    /** Stops waiting for the answer, keeping what was received; an empty answer goes. */
    fun stop() {
        askJob?.cancel()
        askJob = null
        _state.update { s -> s.copy(asking = false, messages = s.messages.filterNot { !it.fromUser && it.text.isEmpty() && it.panel == null && it.error == null }) }
    }

    /** Forgets the conversation; the source and its metric names stay. */
    fun newChat() {
        stop()
        chat?.reset()
        _state.update { it.copy(messages = emptyList()) }
    }

    private fun finish(answerId: Long, panel: PanelSuggestion?, error: String?) {
        updateAnswer(answerId) { it.copy(panel = panel, error = error) }
        _state.update { it.copy(asking = false) }
    }

    private fun updateAnswer(id: Long, change: (ChatMessage) -> ChatMessage) =
        _state.update { s -> s.copy(messages = s.messages.map { if (it.id == id) change(it) else it }) }
}
