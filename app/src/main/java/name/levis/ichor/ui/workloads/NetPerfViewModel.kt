package name.levis.ichor.ui.workloads

import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import name.levis.ichor.data.NetPerfHistory
import name.levis.ichor.data.NetPerfEvent
import name.levis.ichor.data.NetPerfHandle
import name.levis.ichor.data.NetPerfRepository
import name.levis.ichor.model.NetPerfNode
import name.levis.ichor.model.NetPerfProgress
import name.levis.ichor.model.NetPerfReport
import name.levis.ichor.model.NetPerfResult
import name.levis.ichor.model.NetPerfSetup
import name.levis.ichor.model.between
import name.levis.ichor.model.withNodes
import name.levis.ichor.model.withReport
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.uiText

/** A test started from the tab; [running] until the core reports it done. */
data class NetPerfSession(
    val setup: NetPerfSetup,
    val progress: NetPerfProgress? = null,
    val report: NetPerfReport? = null,
    val running: Boolean = true,
    /** Stop was asked: the core is deleting the test namespace. */
    val stopping: Boolean = false,
    val stopped: Boolean = false,
    val error: UiText? = null,
) {
    /** The measurements so far, in the order they ran. */
    val results: List<NetPerfResult> get() = report?.results ?: progress?.results.orEmpty()
}

/**
 * The nodes to test between, the setup, the test running or last run, and the finished tests
 * saved under [scope] (the cluster and the privacy mask).
 */
class NetPerfViewModel(
    private val netPerf: NetPerfRepository,
    private val saved: NetPerfHistory,
    private val scope: String,
) : LoadingViewModel<List<NetPerfNode>>() {
    override suspend fun fetch() = netPerf.nodes()

    private val _setup = MutableStateFlow(NetPerfSetup())
    val setup: StateFlow<NetPerfSetup> = _setup.asStateFlow()

    private val _session = MutableStateFlow<NetPerfSession?>(null)
    val session: StateFlow<NetPerfSession?> = _session.asStateFlow()

    private val _history = MutableStateFlow<List<NetPerfReport>>(emptyList())
    val history: StateFlow<List<NetPerfReport>> = _history.asStateFlow()

    /** A saved test shown instead of the setup. */
    private val _viewing = MutableStateFlow<NetPerfReport?>(null)
    val viewing: StateFlow<NetPerfReport?> = _viewing.asStateFlow()

    private var handle: NetPerfHandle? = null
    private val writes = Mutex()

    init {
        viewModelScope.launch {
            // Unreadable (e.g. its key gone with a restore): a new history starts over it.
            _history.value = withContext(Dispatchers.IO) { runCatching { saved.read(scope) }.getOrDefault(emptyList()) }
        }
    }

    /** Keeps the chosen nodes that are still there and ready, and picks a pair otherwise. */
    fun nodesLoaded(nodes: List<NetPerfNode>) = _setup.update { it.withNodes(nodes) }

    fun update(transform: (NetPerfSetup) -> NetPerfSetup) {
        if (_session.value?.running != true) _setup.update(transform)
    }

    /**
     * A new test from [client] to [server], picked outside the tab (on the topology map): back
     * to the setup with them chosen. Ignored while a test runs.
     */
    fun prepare(client: String, server: String) {
        if (_session.value?.running == true) return
        _session.value = null
        _viewing.value = null
        _setup.update { it.between(client, server, (state.value as? UiState.Loaded)?.data) }
    }

    fun start() {
        val setup = _setup.value
        if (_session.value?.running == true || !setup.ready) return
        // Started here, not in the coroutine: onCleared runs after viewModelScope is
        // cancelled and must still find the handle to stop the test.
        val h = try {
            netPerf.start(setup)
        } catch (e: Exception) {
            _session.value = NetPerfSession(setup, running = false, error = e.uiText())
            return
        }
        handle = h
        _session.value = NetPerfSession(setup)
        viewModelScope.launch {
            for (event in h.events) apply(event)
            handle = null // done (the channel closes after Done): nothing left to stop
        }
    }

    private fun apply(event: NetPerfEvent) {
        when (event) {
            is NetPerfEvent.Progress -> _session.update { it?.copy(progress = event.progress) }
            is NetPerfEvent.Done -> {
                _session.update { s ->
                    s?.copy(
                        report = event.report,
                        running = false,
                        stopped = s.stopping,
                        // Stopping is not a failure, whatever the core reports for it.
                        error = event.error?.takeUnless { s.stopping }?.let(UiText::Raw),
                    )
                }
                // A stopped or failed test is kept too, with what it measured.
                if (event.report.results.isNotEmpty()) {
                    _history.update { it.withReport(event.report) }
                    persist()
                }
            }
        }
    }

    fun open(report: NetPerfReport) {
        _viewing.value = report
    }

    fun close() {
        _viewing.value = null
    }

    fun delete(report: NetPerfReport) {
        _history.update { list -> list.filterNot { it.started == report.started } }
        _viewing.value = null
        persist()
    }

    /** Writes the history as it is now; one write at a time, so the last one wins. */
    private fun persist() {
        viewModelScope.launch(Dispatchers.IO) {
            // A failed write keeps the history on screen until the screen is left.
            writes.withLock { runCatching { saved.save(scope, _history.value) } }
        }
    }

    /** Ends the test early; the core deletes its namespace, then reports it done. */
    fun stop() {
        if (_session.value?.running != true) return
        _session.update { it?.copy(stopping = true) }
        handle?.stop?.invoke()
    }

    /** Back to the setup, forgetting the last test. */
    fun reset() {
        if (_session.value?.running != true) _session.value = null
    }

    override fun onCleared() {
        // Leaving the screen ends the test; the core still deletes its namespace.
        handle?.stop?.invoke()
    }
}
