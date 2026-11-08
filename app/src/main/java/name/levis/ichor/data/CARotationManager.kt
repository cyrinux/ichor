package name.levis.ichor.data

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.channels.ReceiveChannel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import name.levis.ichor.model.CARotationProgress
import name.levis.ichor.ui.FollowedRun

/** What a running CA rotation reports. */
sealed interface CARotationEvent {
    data class Progress(val progress: CARotationProgress) : CARotationEvent

    /** A talosconfig to store for the context: the transition one first, then the [final] one. */
    data class Talosconfig(val yaml: String, val final: Boolean) : CARotationEvent {
        override fun toString() = "Talosconfig(final=$final)" // never the key
    }

    /** It ended; [error] is null when the CA was rotated. */
    data class Done(val error: String?) : CARotationEvent
}

/** A running rotation: its [events] (closed after Done), [proceed] once stored, and [stop]. */
class CARotationHandle(val events: ReceiveChannel<CARotationEvent>, val proceed: () -> Unit, val stop: () -> Unit)

/** The rotation the app follows, of the CA of [context]. */
data class CARotationRunState(
    val context: String,
    val events: List<CARotationProgress> = emptyList(),
    /** The talosconfigs stored so far: the transition one, then the final one. */
    val transitionStored: Boolean = false,
    val finalStored: Boolean = false,
    /** Why the final talosconfig could not be stored (the transition one keeps working). */
    val finalStoreError: String? = null,
    override val finished: Boolean = false,
    override val error: String? = null,
    val stopping: Boolean = false,
) : FollowedRun {
    /** The notification names the cluster: there is no single node. */
    override val hostname: String get() = context
    val last: CARotationProgress? get() = events.lastOrNull()
}

/**
 * Rotating a cluster's Talos API CA (StartTalosCARotation), app-wide like [JoinManager]: leaving
 * the screen keeps it going, one at a time. Each talosconfig the run hands over is stored with
 * [store] (in place of the context's credentials) before the run is told to go on, so the phone
 * never loses the cluster whatever step the run stops at. [onStarted] runs once a rotation is
 * followed (the notification, which keeps the app alive).
 */
class CARotationManager(
    private val startRun: () -> CARotationHandle,
    private val store: suspend (context: String, yaml: String) -> Unit,
    private val activeContext: () -> String,
    private val onStarted: () -> Unit = {},
    private val scope: CoroutineScope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate),
) {
    private val _current = MutableStateFlow<CARotationRunState?>(null)

    /** The rotation running or the last one, until the next starts or [dismiss]. */
    val current: StateFlow<CARotationRunState?> = _current.asStateFlow()
    private var handle: CARotationHandle? = null

    /** Starts rotating the active context's CA; false while one runs. Throws when it cannot start. */
    @Synchronized
    fun start(): Boolean {
        if (_current.value?.running == true) return false
        val context = activeContext()
        val h = startRun()
        handle = h
        _current.value = CARotationRunState(context)
        scope.launch {
            for (event in h.events) apply(context, h, event)
            synchronized(this@CARotationManager) { if (handle === h) handle = null }
        }
        onStarted()
        return true
    }

    /** Stops before the next step: the cluster keeps the CAs it has then. */
    @Synchronized
    fun stop() {
        val h = handle ?: return
        _current.update { if (it?.running == true) it.copy(stopping = true) else it }
        h.stop()
    }

    /** Forgets a finished run. */
    @Synchronized
    fun dismiss() {
        if (_current.value?.finished == true) _current.value = null
    }

    private suspend fun apply(context: String, h: CARotationHandle, event: CARotationEvent) {
        when (event) {
            is CARotationEvent.Progress -> _current.update { it?.copy(events = it.events + event.progress) }
            is CARotationEvent.Talosconfig -> storeConfig(context, h, event)
            is CARotationEvent.Done -> _current.update { it?.copy(finished = true, error = it.error ?: event.error) }
        }
    }

    private suspend fun storeConfig(context: String, h: CARotationHandle, event: CARotationEvent.Talosconfig) {
        val failure = try {
            store(context, event.yaml)
            null
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            e.message ?: e.toString()
        }
        when {
            // The transition one is stored: the run may now change the issuing CA.
            !event.final && failure == null -> {
                _current.update { it?.copy(transitionStored = true) }
                h.proceed()
            }
            // Not stored: stop before the issuing CA changes, the stored config still works.
            !event.final -> {
                _current.update { it?.copy(error = failure, stopping = true) }
                h.stop()
            }
            else -> _current.update { it?.copy(finalStored = failure == null, finalStoreError = failure) }
        }
    }
}
