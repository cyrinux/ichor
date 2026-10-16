package name.levis.ichor.ui.workloads

import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import name.levis.ichor.data.NetPerfHistory
import name.levis.ichor.data.NetPerfManager
import name.levis.ichor.data.NetPerfRepository
import name.levis.ichor.data.NetPerfRunState
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

/** The test of the tab: the one [NetPerfManager] runs (or ran) for its scope, or one that could not start. */
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

private fun NetPerfRunState.session() = NetPerfSession(
    setup = setup,
    progress = progress,
    report = report,
    running = running,
    stopping = stopping,
    stopped = stopped,
    error = error?.let(UiText::Raw),
)

/**
 * The nodes to test between, the setup, the test running or last run, and the finished tests
 * saved under [scope] (the cluster and the privacy mask). The test itself runs in [manager]:
 * leaving the screen keeps it going, and the manager saves it once done.
 */
class NetPerfViewModel(
    private val netPerf: NetPerfRepository,
    private val manager: NetPerfManager,
    private val saved: NetPerfHistory,
    private val scope: String,
) : LoadingViewModel<List<NetPerfNode>>() {
    override suspend fun fetch() = netPerf.nodes()

    private val _setup = MutableStateFlow(NetPerfSetup())
    val setup: StateFlow<NetPerfSetup> = _setup.asStateFlow()

    /** A test that could not start: shown until the next one. */
    private val startFailure = MutableStateFlow<NetPerfSession?>(null)

    private fun sessionOf(run: NetPerfRunState?, failed: NetPerfSession?) = run?.takeIf { it.scope == scope }?.session() ?: failed

    val session: StateFlow<NetPerfSession?> = combine(manager.current, startFailure, ::sessionOf)
        .stateIn(viewModelScope, SharingStarted.Eagerly, sessionOf(manager.current.value, null))

    private val _history = MutableStateFlow<List<NetPerfReport>>(emptyList())
    val history: StateFlow<List<NetPerfReport>> = _history.asStateFlow()

    /** A saved test shown instead of the setup. */
    private val _viewing = MutableStateFlow<NetPerfReport?>(null)
    val viewing: StateFlow<NetPerfReport?> = _viewing.asStateFlow()

    private val writes = Mutex()

    init {
        viewModelScope.launch {
            // Unreadable (e.g. its key gone with a restore): a new history starts over it.
            _history.value = withContext(Dispatchers.IO) { runCatching { saved.read(scope) }.getOrDefault(emptyList()) }
            // A test of this scope that ends shows in the list (the manager saved it).
            manager.current.collect { run ->
                val report = run?.takeIf { it.scope == scope && it.finished }?.report
                if (report != null && report.results.isNotEmpty()) _history.update { it.withReport(report) }
            }
        }
    }

    /** Keeps the chosen nodes that are still there and ready, and picks a pair otherwise. */
    fun nodesLoaded(nodes: List<NetPerfNode>) = _setup.update { it.withNodes(nodes) }

    fun update(transform: (NetPerfSetup) -> NetPerfSetup) {
        if (session.value?.running != true) _setup.update(transform)
    }

    /**
     * A new test from [client] to [server], picked outside the tab (on the topology map): back
     * to the setup with them chosen. Ignored while a test runs.
     */
    fun prepare(client: String, server: String) {
        if (session.value?.running == true) return
        reset()
        _viewing.value = null
        _setup.update { it.between(client, server, (state.value as? UiState.Loaded)?.data) }
    }

    fun start() {
        val setup = _setup.value
        if (session.value?.running == true || !setup.ready) return
        startFailure.value = null
        try {
            manager.start(scope, setup)
        } catch (e: Exception) {
            startFailure.value = NetPerfSession(setup, running = false, error = e.uiText())
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
        if (session.value?.running == true) manager.stop()
    }

    /** Back to the setup, forgetting the last test. */
    fun reset() {
        if (session.value?.running == true) return
        startFailure.value = null
        if (manager.current.value?.scope == scope) manager.dismiss()
    }
}
