package name.levis.ichor.data

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import name.levis.ichor.model.ConfigTryCommand
import name.levis.ichor.model.ConfigTryEvent
import name.levis.ichor.model.ConfigTryState
import name.levis.ichor.model.after
import name.levis.ichor.ui.FollowedRun
import name.levis.ichor.ui.userMessage

/** The machine config try the app follows: at most one at a time, across all nodes. */
data class ConfigTryRunState(
    val node: String,
    override val hostname: String,
    val state: ConfigTryState = ConfigTryState.Running(ConfigTryState.APPLYING),
) : FollowedRun {
    override val finished: Boolean get() = state !is ConfigTryState.Running

    /** Set (maybe empty, when the run said nothing) once the try failed. */
    override val error: String? get() = (state as? ConfigTryState.Failed)?.message

    /** When the node reverts by itself (epoch ms), 0 while not known. */
    val deadline: Long get() = (state as? ConfigTryState.Running)?.deadline ?: 0

    /** The node holds the change and waits: it can be kept or reverted. */
    val trying: Boolean get() = (state as? ConfigTryState.Running)?.trying == true
}

/** How a try is run: TalosRepository.tryMachineConfig, a fake in the tests. */
typealias ConfigTryRunner = (node: String, base: String, draft: String, timeoutSeconds: Int, commands: Flow<ConfigTryCommand>) -> Flow<ConfigTryEvent>

/**
 * A machine config try (StartConfigTry) followed app-wide, like [MaintenanceManager]: leaving the
 * machine config screen keeps the countdown, and Keep or Revert can come from its
 * notification. Talos reverts by itself at the deadline whatever happens to the app.
 * [onStarted] runs once a try is followed (to keep the app alive meanwhile).
 */
class ConfigTryManager(
    private val runTry: ConfigTryRunner,
    private val scope: CoroutineScope = CoroutineScope(SupervisorJob() + Dispatchers.Default),
    private val onStarted: () -> Unit = {},
) {
    private val _current = MutableStateFlow<ConfigTryRunState?>(null)
    val current: StateFlow<ConfigTryRunState?> = _current.asStateFlow()

    // Keep and Revert only matter once the node holds the change, when the run listens.
    private val commands = MutableSharedFlow<ConfigTryCommand>(extraBufferCapacity = 1)

    /** Starts the try unless one is already followed; returns false then. */
    @Synchronized
    fun start(node: String, hostname: String, base: String, draft: String, timeoutSeconds: Int): Boolean {
        if (_current.value?.running == true) return false
        _current.value = ConfigTryRunState(node, hostname)
        scope.launch {
            try {
                runTry(node, base, draft, timeoutSeconds, commands).collect { event ->
                    update(node) { it.copy(state = it.state.after(event)) }
                }
                // The run always ends with how it ended; without it, nothing can be said.
                update(node) { if (it.running) it.copy(state = ConfigTryState.Failed("")) else it }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                update(node) { it.copy(state = ConfigTryState.Failed(e.userMessage())) }
            }
        }
        onStarted()
        return true
    }

    /** Makes the change permanent; only while the node waits. */
    fun keep() = command(ConfigTryCommand.KEEP, ConfigTryState.KEEPING)

    /** Puts the previous config back now; only while the node waits. */
    fun revert() = command(ConfigTryCommand.REVERT, ConfigTryState.REVERTING)

    /** Forgets a finished try. */
    @Synchronized
    fun dismiss() {
        if (_current.value?.finished == true) _current.value = null
    }

    /** Sends [command] once: [phase] shows at once, so a second tap (or button) cannot follow. */
    @Synchronized
    private fun command(command: ConfigTryCommand, phase: String) {
        val run = _current.value ?: return
        val state = run.state as? ConfigTryState.Running ?: return
        if (!state.trying) return
        _current.value = run.copy(state = state.copy(phase = phase, message = ""))
        commands.tryEmit(command)
    }

    private fun update(node: String, change: (ConfigTryRunState) -> ConfigTryRunState) {
        _current.update { if (it?.node == node) change(it) else it }
    }
}
