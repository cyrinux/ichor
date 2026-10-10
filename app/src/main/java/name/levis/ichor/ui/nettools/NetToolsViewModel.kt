package name.levis.ichor.ui.nettools

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import name.levis.ichor.data.NetToolTargets
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.model.NetTool
import name.levis.ichor.model.NetToolEvent
import name.levis.ichor.model.NetToolOptions
import name.levis.ichor.model.NetToolResult
import name.levis.ichor.model.netTargetProblem
import name.levis.ichor.ui.userMessage

/** A check: running with its output lines so far, then its result or why it failed. */
data class NetToolRunState(
    val tool: NetTool,
    val target: String,
    val lines: List<String> = emptyList(),
    val result: NetToolResult? = null,
    val error: String? = null,
) {
    val running: Boolean get() = result == null && error == null
}

/** The output lines kept while a check runs: enough to see what it does. */
private const val MAX_LINES = 200

/** The network tools of one node: one check at a time, the recent targets of the cluster. */
class NetToolsViewModel(
    private val talos: TalosRepository,
    private val targets: NetToolTargets,
    private val node: String,
    private val fingerprint: String,
) : ViewModel() {
    private val _run = MutableStateFlow<NetToolRunState?>(null)
    val run: StateFlow<NetToolRunState?> = _run.asStateFlow()
    private val _recent = MutableStateFlow(targets.of(fingerprint))
    val recent: StateFlow<List<String>> = _recent.asStateFlow()
    private var job: Job? = null

    /** Runs [tool] against [target] unless the target is refused or a check runs. */
    fun start(tool: NetTool, target: String, options: NetToolOptions) {
        if (netTargetProblem(tool, target) != null || _run.value?.running == true) return
        val trimmed = target.trim()
        targets.remember(fingerprint, trimmed)
        _recent.value = targets.of(fingerprint)
        _run.value = NetToolRunState(tool, trimmed)
        job = viewModelScope.launch {
            try {
                talos.netTool(node, tool, trimmed, options).collect { event ->
                    _run.update { state ->
                        state?.let {
                            when (event) {
                                is NetToolEvent.Line -> it.copy(lines = (it.lines + event.text).takeLast(MAX_LINES))
                                is NetToolEvent.Done -> it.copy(result = event.result)
                                is NetToolEvent.Failed -> it.copy(error = event.message)
                            }
                        }
                    }
                }
                _run.update { if (it?.running == true) it.copy(error = "") else it }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                _run.update { it?.copy(error = e.userMessage()) }
            }
        }
    }

    /** Stops the running check: the container is stopped, nothing else changed. */
    fun cancel() {
        if (_run.value?.running != true) return
        job?.cancel()
        _run.value = null
    }

    /** Back to the form after a result. */
    fun clear() {
        if (_run.value?.running == true) return
        _run.value = null
    }
}
