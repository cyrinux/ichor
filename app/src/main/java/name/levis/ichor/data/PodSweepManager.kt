package name.levis.ichor.data

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.channels.ReceiveChannel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import name.levis.ichor.model.KubeSanitizeResult
import name.levis.ichor.model.PodSweepProgress
import name.levis.ichor.model.SanitizeFailure
import name.levis.ichor.ui.FollowedRun

/** What a running pod sweep reports. */
sealed interface PodSweepEvent {
    data class Progress(val progress: PodSweepProgress) : PodSweepEvent

    /** It ended: [result] holds the pods tried so far; [error] when it was refused, could not run or timed out. */
    data class Done(val result: KubeSanitizeResult, val error: String?) : PodSweepEvent
}

/** A running sweep: its [events] (closed after Done) and [stop]. */
class PodSweepHandle(val events: ReceiveChannel<PodSweepEvent>, val stop: () -> Unit)

/**
 * A "Clean up dead pods" sweep: [done] pods tried of [total] in [namespace] ("" for every
 * namespace) of [cluster] (a context name), [deleted] of them gone. [failures] names the pods
 * that could not be deleted once it ended.
 */
data class PodSweepRun(
    val cluster: String,
    val namespace: String,
    val total: Int,
    val done: Int = 0,
    val deleted: Int = 0,
    val failedCount: Int = 0,
    val failures: List<SanitizeFailure> = emptyList(),
    override val running: Boolean = true,
    /** Stop was asked: the core ends after the pod being deleted. */
    val stopping: Boolean = false,
    /** Ended by Stop: some pods were not tried. */
    val stopped: Boolean = false,
    override val error: String? = null,
) : FollowedRun {
    /** The notification names the namespace; the presenter words the "" of every namespace. */
    override val hostname: String get() = namespace
    override val finished: Boolean get() = !running
}

/**
 * Pod sweeps, app-wide like [RunbookManager]: closing the sheet or leaving the Kubernetes
 * screen keeps the sweep going, one at a time. [onStarted] runs once a sweep starts (the
 * notification, which keeps the app alive).
 */
class PodSweepManager(
    private val startRun: (namespace: String, names: String) -> PodSweepHandle,
    private val onStarted: () -> Unit = {},
    private val scope: CoroutineScope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate),
) {
    private val _current = MutableStateFlow<PodSweepRun?>(null)

    /** The sweep running or the last one, until the next starts. */
    val current: StateFlow<PodSweepRun?> = _current.asStateFlow()
    private var handle: PodSweepHandle? = null

    /**
     * Starts deleting [names] (newline-separated "namespace/name", see `sweepNames`) of
     * [namespace] in [cluster]; false while a sweep runs or with nothing to delete. Throws when
     * it cannot start (no cluster).
     */
    @Synchronized
    fun start(cluster: String, namespace: String, names: String): Boolean {
        val total = names.lines().count { it.isNotBlank() }
        if (_current.value?.running == true || total == 0) return false
        val h = startRun(namespace, names)
        handle = h
        _current.value = PodSweepRun(cluster, namespace, total)
        scope.launch {
            for (event in h.events) apply(event)
            synchronized(this@PodSweepManager) { if (handle === h) handle = null }
        }
        onStarted()
        return true
    }

    /** Ends the sweep after the pod being deleted. */
    @Synchronized
    fun stop() {
        val h = handle ?: return
        _current.update { if (it?.running == true) it.copy(stopping = true) else it }
        h.stop()
    }

    private fun apply(event: PodSweepEvent) {
        _current.update { s -> s?.let { applied(it, event) } }
    }

    private fun applied(s: PodSweepRun, event: PodSweepEvent): PodSweepRun = when (event) {
        is PodSweepEvent.Progress -> event.progress.let { p ->
            s.copy(done = p.done, total = p.total.takeIf { it > 0 } ?: s.total, deleted = p.done - p.failed, failedCount = p.failed)
        }
        is PodSweepEvent.Done -> event.result.let { r ->
            s.copy(
                done = r.deleted + r.failed.size,
                deleted = r.deleted,
                failedCount = r.failed.size,
                failures = r.failed,
                running = false,
                stopped = r.stopped,
                error = event.error,
            )
        }
    }
}
