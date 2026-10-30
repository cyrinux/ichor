package name.levis.ichor.data

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import name.levis.ichor.model.RunbookPlan
import name.levis.ichor.model.RunbookStepState
import name.levis.ichor.ui.FollowedRun

/** The runbook the app runs: at most one at a time, with each step's last state. */
data class RunbookRunState(
    val plan: RunbookPlan,
    /** What it was asked for: its header shows them. */
    val values: Map<String, String> = emptyMap(),
    val steps: List<RunbookStepState> = plan.steps.mapIndexed { i, s -> RunbookStepState(index = i, id = s.id) },
    /** The run's end: every step ran or was skipped, all ok. */
    val ok: Boolean = false,
    override val finished: Boolean = false,
    override val error: String? = null,
    /** Stop was asked. */
    val stopping: Boolean = false,
) : FollowedRun {
    /** The notification names the runbook. */
    override val hostname: String get() = plan.name

    /** The step waiting for the user's confirmation, if any. */
    val waiting: RunbookStepState? get() = if (finished) null else steps.firstOrNull { it.waiting }

    /** The step running or waiting now (from 0), or the number of steps that ended. */
    val current: Int get() = steps.indexOfFirst { !it.ended }.takeIf { it >= 0 } ?: steps.size

    val total: Int get() = steps.size
}

/**
 * Runbook runs, app-wide like [NetPerfManager]: leaving the screen keeps the run going (the
 * screen reads [current]); a destructive step waits in the core until [confirm]. [onStarted]
 * runs once a run starts (the notification).
 */
class RunbookManager(
    private val startRun: (yaml: String, continueIds: Set<String>, values: Map<String, String>) -> RunbookHandle,
    private val onStarted: () -> Unit = {},
    private val scope: CoroutineScope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate),
) {
    private val _current = MutableStateFlow<RunbookRunState?>(null)
    val current: StateFlow<RunbookRunState?> = _current.asStateFlow()
    private var handle: RunbookHandle? = null

    /** Starts [yaml] with [values] (whose plan is [plan]) unless a runbook runs; false then. Throws when it cannot start. */
    @Synchronized
    fun start(yaml: String, plan: RunbookPlan, continueIds: Set<String>, values: Map<String, String> = emptyMap()): Boolean {
        if (_current.value?.running == true) return false
        val h = startRun(yaml, continueIds, values)
        handle = h
        _current.value = RunbookRunState(plan, values)
        scope.launch {
            for (event in h.events) apply(event)
            synchronized(this@RunbookManager) { if (handle === h) handle = null }
        }
        onStarted()
        return true
    }

    /** Lets the waiting step [index] run. */
    fun confirm(index: Int) {
        val state = _current.value ?: return
        if (state.waiting?.index != index) return
        handle?.confirm?.invoke(index)
    }

    @Synchronized
    fun stop() {
        val h = handle ?: return
        _current.update { if (it?.running == true) it.copy(stopping = true) else it }
        h.stop()
    }

    /** Forgets an ended run (its screen closed). */
    fun clear() {
        _current.update { if (it?.running == true) it else null }
    }

    private fun apply(event: RunbookEvent) {
        _current.update { s ->
            when {
                s == null -> null
                event is RunbookEvent.Step -> s.copy(steps = s.steps.replaced(event.state))
                event is RunbookEvent.Done -> s.copy(
                    steps = event.result.steps.takeIf { it.size == s.steps.size } ?: s.steps,
                    ok = event.result.ok,
                    finished = true,
                )
                event is RunbookEvent.Failed -> s.copy(finished = true, error = event.message)
                else -> s
            }
        }
    }

    private fun List<RunbookStepState>.replaced(state: RunbookStepState): List<RunbookStepState> =
        mapIndexed { i, old -> if (i == state.index) state else old }
}
