package name.levis.ichor.ui.workloads

import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import name.levis.ichor.data.NetPerfEvent
import name.levis.ichor.data.NetPerfHandle
import name.levis.ichor.data.NetPerfRepository
import name.levis.ichor.model.NetPerfNode
import name.levis.ichor.model.NetPerfProgress
import name.levis.ichor.model.NetPerfReport
import name.levis.ichor.model.NetPerfResult
import name.levis.ichor.model.NetPerfSetup
import name.levis.ichor.model.withNodes
import name.levis.ichor.ui.LoadingViewModel
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

/** The nodes to test between, the setup, and the test running or last run. */
class NetPerfViewModel(private val netPerf: NetPerfRepository) : LoadingViewModel<List<NetPerfNode>>() {
    override suspend fun fetch() = netPerf.nodes()

    private val _setup = MutableStateFlow(NetPerfSetup())
    val setup: StateFlow<NetPerfSetup> = _setup.asStateFlow()

    private val _session = MutableStateFlow<NetPerfSession?>(null)
    val session: StateFlow<NetPerfSession?> = _session.asStateFlow()

    private var handle: NetPerfHandle? = null

    /** Keeps the chosen nodes that are still there and ready, and picks a pair otherwise. */
    fun nodesLoaded(nodes: List<NetPerfNode>) = _setup.update { it.withNodes(nodes) }

    fun update(transform: (NetPerfSetup) -> NetPerfSetup) {
        if (_session.value?.running != true) _setup.update(transform)
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

    private fun apply(event: NetPerfEvent) = when (event) {
        is NetPerfEvent.Progress -> _session.update { it?.copy(progress = event.progress) }
        is NetPerfEvent.Done -> _session.update { s ->
            s?.copy(
                report = event.report,
                running = false,
                stopped = s.stopping,
                // Stopping is not a failure, whatever the core reports for it.
                error = event.error?.takeUnless { s.stopping }?.let(UiText::Raw),
            )
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
