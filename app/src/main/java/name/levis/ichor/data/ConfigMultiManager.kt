package name.levis.ichor.data

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import name.levis.ichor.model.ConfigApplyMode
import name.levis.ichor.model.ConfigEdit
import name.levis.ichor.model.MultiApplyRun
import name.levis.ichor.model.MultiConfigEvent
import name.levis.ichor.ui.FollowedRun
import name.levis.ichor.ui.userMessage

/**
 * The multi-node config apply the app follows: at most one at a time. [origin] is the node whose
 * machine config screen started it (where it shows again); [hostname] names the run in its
 * notification (the cluster).
 */
data class ConfigMultiRunState(
    val origin: String,
    override val hostname: String,
    val run: MultiApplyRun,
) : FollowedRun {
    override val finished: Boolean get() = run.finished
    override val error: String? get() = run.error
}

/** How a multi-node apply is run: TalosRepository.applyMachineConfigMulti, a fake in the tests. */
typealias ConfigMultiRunner = (nodes: List<String>, edits: List<ConfigEdit>, mode: ConfigApplyMode) -> Flow<MultiConfigEvent>

/**
 * A machine config change applied to several nodes (StartConfigApplyMulti), followed app-wide
 * like [MaintenanceManager]: a reboot rollout waits minutes per node, and leaving the screen or
 * locking the phone must not cut it off halfway. [onStarted] runs once a run is followed (to keep
 * the app alive meanwhile).
 */
class ConfigMultiManager(
    private val runMulti: ConfigMultiRunner,
    private val scope: CoroutineScope = CoroutineScope(SupervisorJob() + Dispatchers.Default),
    private val onStarted: () -> Unit = {},
) {
    private val _current = MutableStateFlow<ConfigMultiRunState?>(null)
    val current: StateFlow<ConfigMultiRunState?> = _current.asStateFlow()

    /** Starts the run unless one is already followed; returns false then. */
    @Synchronized
    fun start(origin: String, label: String, nodes: List<String>, edits: List<ConfigEdit>, mode: ConfigApplyMode): Boolean {
        if (_current.value?.running == true || nodes.isEmpty()) return false
        _current.value = ConfigMultiRunState(origin, label, MultiApplyRun(mode))
        scope.launch {
            try {
                runMulti(nodes, edits, mode).collect { event -> update { it.copy(run = it.run.after(event)) } }
                // The run always ends with how it ended; without it, nothing can be said.
                update { if (it.running) it.copy(run = it.run.copy(finished = true, error = "")) else it }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                update { it.copy(run = it.run.copy(finished = true, error = e.userMessage())) }
            }
        }
        onStarted()
        return true
    }

    /** Forgets a finished run. */
    @Synchronized
    fun dismiss() {
        if (_current.value?.finished == true) _current.value = null
    }

    private fun update(change: (ConfigMultiRunState) -> ConfigMultiRunState) {
        _current.update { it?.let(change) }
    }
}
