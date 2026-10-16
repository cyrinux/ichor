package name.levis.ichor.data

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import name.levis.ichor.model.NetPerfProgress
import name.levis.ichor.model.NetPerfReport
import name.levis.ichor.model.NetPerfResult
import name.levis.ichor.model.NetPerfSetup
import name.levis.ichor.ui.FollowedRun

/** The network test the app runs: at most one at a time. [scope]: the history it is saved under. */
data class NetPerfRunState(
    val scope: String,
    val setup: NetPerfSetup,
    val progress: NetPerfProgress? = null,
    val report: NetPerfReport? = null,
    override val finished: Boolean = false,
    override val error: String? = null,
    /** Stop was asked: the core is deleting the test namespace. */
    val stopping: Boolean = false,
) : FollowedRun {
    /** The notification names the pair of nodes. */
    override val hostname: String get() = "${setup.client} → ${setup.server}"

    /** Ended by Stop, not on its own. */
    val stopped: Boolean get() = finished && stopping

    /** The measurements so far, in the order they ran. */
    val results: List<NetPerfResult> get() = report?.results ?: progress?.results.orEmpty()
}

/**
 * Network tests, app-wide like [ImagePullManager]: leaving the screen keeps the test going (the
 * screen reads [current]). The Go core deletes the test namespace once the test ends, fails or
 * is stopped, before it reports it done. A finished test with measurements is handed to [save]
 * (the history of its scope), whoever is watching. [onStarted] runs once a test runs.
 */
class NetPerfManager(
    private val startRun: (NetPerfSetup) -> NetPerfHandle,
    private val save: suspend (scope: String, report: NetPerfReport) -> Unit,
    private val onStarted: () -> Unit = {},
    private val scope: CoroutineScope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate),
) {
    private val _current = MutableStateFlow<NetPerfRunState?>(null)
    val current: StateFlow<NetPerfRunState?> = _current.asStateFlow()
    private var handle: NetPerfHandle? = null

    /**
     * Starts [setup] for the history [historyScope] unless a test runs; returns false then.
     * Throws when the test cannot start (no cluster to call).
     */
    @Synchronized
    fun start(historyScope: String, setup: NetPerfSetup): Boolean {
        if (_current.value?.running == true || !setup.ready) return false
        val h = startRun(setup)
        handle = h
        _current.value = NetPerfRunState(historyScope, setup)
        scope.launch {
            for (event in h.events) apply(historyScope, event)
            synchronized(this@NetPerfManager) { if (handle === h) handle = null } // nothing left to stop
        }
        onStarted()
        return true
    }

    private suspend fun apply(historyScope: String, event: NetPerfEvent) {
        when (event) {
            is NetPerfEvent.Progress -> _current.update { if (it?.running == true) it.copy(progress = event.progress) else it }
            is NetPerfEvent.Done -> {
                _current.update { s ->
                    s?.copy(
                        report = event.report,
                        finished = true,
                        // Stopping is not a failure, whatever the core reports for it.
                        error = event.error?.takeUnless { s.stopping },
                    )
                }
                // A stopped or failed test is kept too, with what it measured.
                if (event.report.results.isNotEmpty()) save(historyScope, event.report)
            }
        }
    }

    /** Ends the test early; the core deletes its namespace, then reports it done. */
    @Synchronized
    fun stop() {
        if (_current.value?.let { it.running && !it.stopping } != true) return
        _current.update { it?.copy(stopping = true) }
        handle?.stop?.invoke()
    }

    /** Forgets a finished test (back to the setup). */
    @Synchronized
    fun dismiss() {
        if (_current.value?.finished == true) _current.value = null
    }
}
