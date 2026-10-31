package name.levis.ichor.ui.logsearch

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import name.levis.ichor.data.LogsBackendRepository
import name.levis.ichor.data.LogsBackendStore
import name.levis.ichor.model.LOG_SEARCH_DEFAULT_HOURS
import name.levis.ichor.model.LOGS_KIND_LOKI
import name.levis.ichor.model.LogsBackendConfig
import name.levis.ichor.model.LogsResult
import name.levis.ichor.model.PromSource
import name.levis.ichor.model.savedWith
import name.levis.ichor.model.withLabelMatcher
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.cancellableCatching
import name.levis.ichor.ui.uiText
import name.levis.ichor.ui.userMessage

data class LogSearchState(
    /** The source was read from the store and, without one, searched for once. */
    val ready: Boolean = false,
    /** Where the search goes; null when none was chosen nor found. */
    val source: PromSource? = null,
    /** Log backends found in the cluster; null before a search for them. */
    val discovered: List<PromSource>? = null,
    val discovering: Boolean = false,
    val discoveryError: String? = null,
    val query: String = "",
    val hours: Int = LOG_SEARCH_DEFAULT_HOURS,
    /** Null before the first search. */
    val result: UiState<LogsResult>? = null,
    val saved: List<String> = emptyList(),
    /** The label whose values the picker lists; null while it lists the label names. */
    val pickedLabel: String? = null,
    val picker: UiState<List<String>> = UiState.Loading,
)

/**
 * The Log search screen of the cluster [fingerprint]: its log backend (the one chosen in [store],
 * else the likeliest found), a query over a time range, its saved queries. [initialQuery] is
 * searched as soon as the source is known; the [demo] cluster starts on a sample query.
 */
class LogSearchViewModel(
    private val logs: LogsBackendRepository,
    private val store: LogsBackendStore,
    private val fingerprint: String,
    private val initialQuery: String,
    private val demo: Boolean,
) : ViewModel() {
    private val _state = MutableStateFlow(LogSearchState(query = initialQuery))
    val state: StateFlow<LogSearchState> = _state.asStateFlow()
    private var searchJob: Job? = null
    private var pickerJob: Job? = null

    fun load() = viewModelScope.launch {
        if (_state.value.ready) return@launch
        try {
            val config = withContext(Dispatchers.IO) { store.read(fingerprint) }
            _state.update { it.copy(saved = config.saved) }
            if (config.source != null) {
                _state.update { it.copy(ready = true, source = config.source) }
                searchFirst()
            } else {
                discover(useFirst = true).join()
            }
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            _state.update { it.copy(ready = true, discoveryError = e.userMessage()) }
        }
    }

    /** Looks for log backends; [useFirst] takes the likeliest one when none was chosen. */
    fun discover(useFirst: Boolean = false) = viewModelScope.launch {
        _state.update { it.copy(discovering = true, discoveryError = null) }
        try {
            val sources = logs.discover()
            _state.update { it.copy(discovered = sources, discovering = false) }
            if (useFirst) {
                _state.update { it.copy(ready = true, source = sources.firstOrNull()) }
                searchFirst()
            }
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            _state.update { it.copy(ready = true, discovering = false, discoveryError = e.userMessage()) }
        }
    }

    private fun searchFirst() {
        val source = _state.value.source ?: return
        if (demo && _state.value.query.isBlank() && source.kind == LOGS_KIND_LOKI) _state.update { it.copy(query = DEMO_QUERY) }
        if (_state.value.query.isNotBlank()) search()
    }

    /** Checks [source] answers, saves it as the cluster's. Null when saved, else why not. */
    suspend fun setSource(source: PromSource): String? = try {
        val checked = logs.probe(source)
        withContext(Dispatchers.IO) { save { it.copy(source = checked) } }
        _state.update { it.copy(source = checked, result = null) }
        searchFirst()
        null
    } catch (e: CancellationException) {
        throw e
    } catch (e: Exception) {
        e.userMessage()
    }

    /** Null when [source] answers, else why not. */
    suspend fun test(source: PromSource): String? = try {
        logs.probe(source)
        null
    } catch (e: CancellationException) {
        throw e
    } catch (e: Exception) {
        e.userMessage()
    }

    fun setQuery(query: String) = _state.update { it.copy(query = query) }

    /** Searches [hours] back from now, if there is a query. */
    fun setHours(hours: Int) {
        _state.update { it.copy(hours = hours) }
        if (_state.value.query.isNotBlank()) search()
    }

    fun openSaved(query: String) {
        _state.update { it.copy(query = query) }
        search()
    }

    fun search() {
        val s = _state.value
        val source = s.source ?: return
        if (s.query.isBlank()) return
        searchJob?.cancel()
        _state.update { it.copy(result = UiState.Loading) }
        searchJob = viewModelScope.launch {
            val end = System.currentTimeMillis()
            val outcome = cancellableCatching { logs.query(source, s.query, end - s.hours * MILLIS_PER_HOUR, end) }
            _state.update { it.copy(result = outcome.fold({ r -> UiState.Loaded(r) }, { e -> UiState.Failed(e.uiText()) })) }
        }
    }

    /** Saves the current query, or forgets it when it is saved already. */
    fun toggleSaved() {
        val query = _state.value.query.trim()
        if (query.isEmpty()) return
        val saved = _state.value.saved
        val next = if (query in saved) saved - query else savedWith(saved, query)
        _state.update { it.copy(saved = next) }
        viewModelScope.launch(Dispatchers.IO) { save { it.copy(saved = next) } }
    }

    private fun save(change: (LogsBackendConfig) -> LogsBackendConfig) = store.save(fingerprint, change(store.read(fingerprint)))

    /** Opens the picker on the label names. */
    fun pickLabels() = readPicker(null)

    /** Moves the picker to the values of [label]. */
    fun pickLabel(label: String) = readPicker(label)

    private fun readPicker(label: String?) {
        val source = _state.value.source ?: return
        pickerJob?.cancel()
        _state.update { it.copy(pickedLabel = label, picker = UiState.Loading) }
        pickerJob = viewModelScope.launch {
            val outcome = cancellableCatching { logs.labels(source, label.orEmpty()) }
            _state.update { it.copy(picker = outcome.fold({ v -> UiState.Loaded(v) }, { e -> UiState.Failed(e.uiText()) })) }
        }
    }

    /** Adds the picked label's [value] to the query. */
    fun pickValue(value: String) {
        val label = _state.value.pickedLabel ?: return
        _state.update { it.copy(query = withLabelMatcher(it.source?.kind.orEmpty(), it.query, label, value)) }
    }

    private companion object {
        const val MILLIS_PER_HOUR = 3_600_000L
        const val DEMO_QUERY = "{namespace=\"demo-shop\"}"
    }
}
