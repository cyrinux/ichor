package name.levis.ichor.data

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import name.levis.ichor.model.K8sUpgradeProgress
import name.levis.ichor.ui.FollowedRun
import name.levis.ichor.ui.goErrorText

/** The Kubernetes upgrade the app follows. [hostname] names it in the notification (the cluster). */
data class K8sUpgradeRunState(
    val version: String,
    override val hostname: String,
    val dryRun: Boolean,
    /** Every step reported so far, oldest first. */
    val events: List<K8sUpgradeProgress> = emptyList(),
    override val finished: Boolean = false,
    override val error: String? = null,
    /** Cancel was asked: the run ends after the node it is changing. */
    val cancelling: Boolean = false,
) : FollowedRun {
    val latest: K8sUpgradeProgress? get() = events.lastOrNull()
}

/** A started run, which can be cancelled between nodes. */
fun interface K8sUpgradeHandle {
    fun cancel()
}

/** Starts the run in the Go core and reports to the two callbacks; a fake in the tests. */
typealias K8sUpgradeStarter = (version: String, dryRun: Boolean, onProgress: (String) -> Unit, onDone: (String) -> Unit) -> K8sUpgradeHandle

/**
 * The Kubernetes upgrade (StartK8sUpgrade), followed app-wide like [ClusterUpgradeManager]:
 * each control plane then each kubelet is waited for, so leaving the screen keeps it going and
 * a foreground service keeps the app alive. [onStarted] runs once a run is followed.
 */
class K8sUpgradeManager(
    private val startRun: K8sUpgradeStarter,
    private val onStarted: () -> Unit = {},
) {
    private val _current = MutableStateFlow<K8sUpgradeRunState?>(null)
    val current: StateFlow<K8sUpgradeRunState?> = _current.asStateFlow()
    private var handle: K8sUpgradeHandle? = null

    /** Starts the run unless one is followed; returns false then. [label] names it (the cluster). */
    @Synchronized
    fun start(version: String, label: String, dryRun: Boolean): Boolean {
        if (_current.value?.running == true) return false
        _current.value = K8sUpgradeRunState(version, label, dryRun)
        handle = try {
            startRun(version, dryRun, ::onProgress, ::onDone)
        } catch (e: Exception) {
            _current.value = null
            throw e
        }
        onStarted()
        return true
    }

    private fun onProgress(json: String) {
        val event = runCatching { TalosJson.decodeFromString(K8sUpgradeProgress.serializer(), json) }.getOrNull() ?: return
        _current.update { if (it?.running == true) it.copy(events = it.events + event) else it }
    }

    private fun onDone(errMessage: String) {
        _current.update { if (it?.running == true) it.copy(finished = true, error = errMessage.ifEmpty { null }?.let(::goErrorText)) else it }
    }

    /** Stops after the node being changed: the nodes done keep the new version. */
    @Synchronized
    fun cancel() {
        if (_current.value?.running != true) return
        _current.update { it?.copy(cancelling = true) }
        handle?.cancel()
    }

    /** Forgets a finished run. */
    @Synchronized
    fun dismiss() {
        if (_current.value?.finished == true) {
            handle = null
            _current.value = null
        }
    }
}
