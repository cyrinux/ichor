package name.levis.ichor.ui.metrics

import androidx.annotation.StringRes
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.async
import kotlinx.coroutines.awaitAll
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.Semaphore
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.sync.withPermit
import kotlinx.coroutines.withContext
import name.levis.ichor.R
import name.levis.ichor.data.MetricsStore
import name.levis.ichor.data.KubeRepository
import name.levis.ichor.model.MetricsConfig
import name.levis.ichor.model.PromPanel
import name.levis.ichor.model.PromResult
import name.levis.ichor.model.PromSource
import name.levis.ichor.ui.userMessage
import java.util.UUID

/** The time ranges offered above the panels. */
enum class MetricsRange(val seconds: Long, @StringRes val label: Int) {
    MINUTES_15(15 * 60, R.string.metrics_range_15m),
    HOUR_1(60 * 60, R.string.metrics_range_1h),
    HOURS_6(6 * 60 * 60, R.string.metrics_range_6h),
    DAY_1(24 * 60 * 60, R.string.metrics_range_1d),
    DAYS_7(7 * 24 * 60 * 60, R.string.metrics_range_7d),
}

/** A panel's last answer: [result] stays shown while it reloads or when a reload fails. */
data class PanelResult(val result: PromResult? = null, val error: String? = null, val loading: Boolean = false)

data class MetricsState(
    val loaded: Boolean = false,
    val config: MetricsConfig = MetricsConfig(),
    val range: MetricsRange = MetricsRange.HOUR_1,
    val results: Map<String, PanelResult> = emptyMap(),
    val presets: List<PromPanel> = emptyList(),
    /** Query APIs found in the cluster; null before a search. */
    val discovered: List<PromSource>? = null,
    val discovering: Boolean = false,
    val discoveryError: String? = null,
    val error: String? = null,
)

/**
 * The Metrics screen of the cluster [fingerprint]: its source and panels from [store], each
 * panel queried over the chosen range. On first open it looks for a query API in the
 * cluster and, when it finds one, starts with the built-in panels.
 */
class MetricsViewModel(
    private val kube: KubeRepository,
    private val store: MetricsStore,
    private val fingerprint: String,
    /** The message when a query is tried before a source is set. */
    private val noSource: String,
) : ViewModel() {
    private val _state = MutableStateFlow(MetricsState())
    val state: StateFlow<MetricsState> = _state.asStateFlow()
    private var refreshJob: Job? = null
    private val saving = Mutex()

    fun load() = viewModelScope.launch {
        // Once: a recreated screen must not reload an older config over unsaved edits.
        if (_state.value.loaded) return@launch
        try {
            val config = withContext(Dispatchers.IO) { store.read(fingerprint) }
            val presets = kube.promPresets()
            _state.update { it.copy(loaded = true, config = config, presets = presets) }
            if (config.source == null) discover(autoSelect = true) else refresh()
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            _state.update { it.copy(loaded = true, error = e.userMessage()) }
        }
    }

    /** Looks for query APIs; [autoSelect] takes the likeliest one when none is set yet. */
    fun discover(autoSelect: Boolean = false) = viewModelScope.launch {
        _state.update { it.copy(discovering = true, discoveryError = null) }
        try {
            val sources = kube.promDiscover()
            _state.update { it.copy(discovered = sources, discovering = false) }
            val first = sources.firstOrNull()
            if (autoSelect && first != null && _state.value.config.source == null) setSource(first)
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            _state.update { it.copy(discovering = false, discoveryError = e.userMessage()) }
        }
    }

    /** Checks [source] (Go), saves it and reloads; the first source brings the built-in panels. Null when saved, else why not. */
    suspend fun setSource(source: PromSource): String? = try {
        val checked = kube.normalizePromSource(source)
        val current = _state.value.config
        val panels = current.panels.ifEmpty { _state.value.presets.map { it.copy(id = newId()) } }
        save(current.copy(source = checked, panels = panels))
        refresh()
        null
    } catch (e: CancellationException) {
        throw e
    } catch (e: Exception) {
        e.userMessage()
    }

    /** Runs a trivial query against [source]: null when it answers, else why not. */
    suspend fun test(source: PromSource): String? = try {
        val checked = kube.normalizePromSource(source)
        val now = System.currentTimeMillis() / 1000
        kube.promRange(checked, "vector(1)", now - 60, now)
        null
    } catch (e: CancellationException) {
        throw e
    } catch (e: Exception) {
        e.userMessage()
    }

    /** One query of [panel] over the current range, for the editor's preview. */
    suspend fun preview(panel: PromPanel): Result<PromResult> {
        val source = _state.value.config.source ?: return Result.failure(IllegalStateException(noSource))
        return runQuery(source, panel.query)
    }

    fun setRange(range: MetricsRange) {
        if (range == _state.value.range) return
        _state.update { it.copy(range = range) }
        refresh()
    }

    /** Adds [panel], or replaces the one with its id. */
    fun savePanel(panel: PromPanel) {
        val stored = panel.copy(id = panel.id.ifBlank { newId() })
        val panels = _state.value.config.panels
        val updated = if (panels.any { it.id == stored.id }) panels.map { if (it.id == stored.id) stored else it } else panels + stored
        save(_state.value.config.copy(panels = updated))
        refreshPanel(stored)
    }

    fun deletePanel(id: String) {
        save(_state.value.config.copy(panels = _state.value.config.panels.filterNot { it.id == id }))
        _state.update { it.copy(results = it.results - id) }
    }

    /** Moves the panel [id] by [delta] places (−1 up, +1 down). */
    fun movePanel(id: String, delta: Int) {
        val panels = _state.value.config.panels
        val from = panels.indexOfFirst { it.id == id }
        val to = from + delta
        if (from < 0 || to !in panels.indices) return
        val reordered = panels.toMutableList().apply { add(to, removeAt(from)) }.toList()
        save(_state.value.config.copy(panels = reordered))
    }

    /** Reloads every panel, four at a time; [quiet] keeps the loading marks off (auto refresh). */
    fun refresh(quiet: Boolean = false) {
        val source = _state.value.config.source ?: return
        val panels = _state.value.config.panels
        refreshJob?.cancel()
        refreshJob = viewModelScope.launch {
            val permits = Semaphore(CONCURRENT_QUERIES)
            panels.map { panel -> async { permits.withPermit { query(source, panel, quiet) } } }.awaitAll()
        }
    }

    private fun refreshPanel(panel: PromPanel) {
        val source = _state.value.config.source ?: return
        viewModelScope.launch { query(source, panel, quiet = false) }
    }

    private suspend fun query(source: PromSource, panel: PromPanel, quiet: Boolean) {
        if (!quiet) setResult(panel.id) { it.copy(loading = true) }
        val outcome = runQuery(source, panel.query)
        // An answer for a panel since edited or deleted, or for the previous source, is stale.
        val config = _state.value.config
        if (config.source != source || panel !in config.panels) return
        setResult(panel.id) { previous ->
            outcome.fold(
                onSuccess = { PanelResult(result = it) },
                onFailure = { previous.copy(error = it.userMessage(), loading = false) },
            )
        }
    }

    private suspend fun runQuery(source: PromSource, query: String): Result<PromResult> {
        val end = System.currentTimeMillis() / 1000
        return try {
            Result.success(kube.promRange(source, query, end - _state.value.range.seconds, end))
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            Result.failure(e)
        }
    }

    private fun setResult(id: String, change: (PanelResult) -> PanelResult) =
        _state.update { it.copy(results = it.results + (id to change(it.results[id] ?: PanelResult()))) }

    /** Shown at once; written in order, always the latest config. */
    private fun save(config: MetricsConfig) {
        _state.update { it.copy(config = config) }
        viewModelScope.launch(Dispatchers.IO) {
            try {
                // Written even when the screen closes right after the edit.
                withContext(NonCancellable) { saving.withLock { store.save(fingerprint, _state.value.config) } }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                _state.update { it.copy(error = e.userMessage()) }
            }
        }
    }

    private companion object {
        const val CONCURRENT_QUERIES = 4

        fun newId() = UUID.randomUUID().toString()
    }
}
