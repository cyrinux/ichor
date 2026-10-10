package name.levis.ichor.data

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import name.levis.ichor.model.ClusterUpgradeCommand
import name.levis.ichor.model.ClusterUpgradeNode
import name.levis.ichor.model.ClusterUpgradeProgress
import name.levis.ichor.ui.FollowedRun
import name.levis.ichor.ui.goErrorText

/** The rolling cluster upgrade the app follows. [hostname] names it in the notification (the cluster). */
data class ClusterUpgradeRunState(
    val version: String,
    override val hostname: String,
    val progress: ClusterUpgradeProgress? = null,
    override val finished: Boolean = false,
    override val error: String? = null,
    /** Abort was asked: the roll ends before its next node. */
    val aborting: Boolean = false,
) : FollowedRun {
    val paused: Boolean get() = !finished && progress?.phase == ClusterUpgradeProgress.PAUSED
    val nodes: List<ClusterUpgradeNode> get() = progress?.nodes.orEmpty()
}

/** A started roll: what the run screen and the notification can ask of it. */
interface ClusterUpgradeHandle {
    fun send(command: ClusterUpgradeCommand)

    /** Stops following; the node being upgraded goes on, the lock expires within minutes. */
    fun cancel()
}

/** Starts the roll in the Go core and reports to the two callbacks; a fake in the tests. */
typealias ClusterUpgradeStarter = (
    version: String,
    drain: Boolean,
    acknowledged: Boolean,
    onProgress: (String) -> Unit,
    onDone: (String) -> Unit,
) -> ClusterUpgradeHandle

/**
 * The rolling upgrade of every node (StartClusterUpgrade), followed app-wide like
 * [UpgradeManager]: a roll takes many minutes per node, so leaving the screen keeps it going
 * and a foreground service keeps the app alive. [onStarted] runs once a roll is followed.
 */
class ClusterUpgradeManager(
    private val startRun: ClusterUpgradeStarter,
    private val onStarted: () -> Unit = {},
) {
    private val _current = MutableStateFlow<ClusterUpgradeRunState?>(null)
    val current: StateFlow<ClusterUpgradeRunState?> = _current.asStateFlow()
    private var handle: ClusterUpgradeHandle? = null

    /** Starts the roll unless one is followed; returns false then. [label] names it (the cluster). */
    @Synchronized
    fun start(version: String, label: String, drain: Boolean, acknowledged: Boolean): Boolean {
        if (_current.value?.running == true) return false
        _current.value = ClusterUpgradeRunState(version, label)
        handle = try {
            startRun(version, drain, acknowledged, ::onProgress, ::onDone)
        } catch (e: Exception) {
            _current.value = null
            throw e
        }
        onStarted()
        return true
    }

    private fun onProgress(json: String) {
        val event = runCatching { TalosJson.decodeFromString(ClusterUpgradeProgress.serializer(), json) }.getOrNull() ?: return
        _current.update { if (it?.running == true) it.copy(progress = event) else it }
    }

    private fun onDone(errMessage: String) {
        _current.update { if (it?.running == true) it.copy(finished = true, error = errMessage.ifEmpty { null }?.let(::goErrorText)) else it }
    }

    /** Pause before the next node; Resume (also retries a failed gate). */
    fun pause() = send(ClusterUpgradeCommand.PAUSE)

    fun resume() = send(ClusterUpgradeCommand.RESUME)

    /** Ends the roll before its next node: the nodes upgraded stay upgraded. */
    @Synchronized
    fun abort() {
        if (_current.value?.running != true) return
        _current.update { it?.copy(aborting = true) }
        handle?.send(ClusterUpgradeCommand.ABORT)
    }

    @Synchronized
    private fun send(command: ClusterUpgradeCommand) {
        if (_current.value?.running == true) handle?.send(command)
    }

    /** Forgets a finished roll. */
    @Synchronized
    fun dismiss() {
        if (_current.value?.finished == true) {
            handle = null
            _current.value = null
        }
    }
}
