package name.levis.ichor.data

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.launch

/**
 * Keeps streamed rows (events, followed logs, watches) from outliving a screenshot mode switch:
 * held by what keeps the rows (a view model), [run] stops the stream when a switch starts,
 * drops the rows, and starts it again once the switch is over, under the new setting. A switch
 * that happened while nothing was collected (the screen in the back stack) drops them too.
 */
class MaskScoped(private val epoch: StateFlow<Int> = MaskEpoch.value) {
    private var seen = epoch.value

    /** Runs [block] (the stream) outside switches; [clear] drops what it showed, once per switch. */
    suspend fun run(clear: () -> Unit, block: suspend () -> Unit) {
        epoch.collectLatest { current ->
            // Odd: a switch started; even: it is over (its start may have been missed).
            if (current != seen) {
                seen = current
                clear()
            }
            if (current % 2 == 0) block()
        }
    }

    /**
     * For what runs once rather than while collected (a health check run): [onSwitch] at the
     * start (switching true) and at the end of each switch from now on (cancel the run, drop
     * what it showed, maybe read again); only the end when the start was missed.
     */
    fun watch(scope: CoroutineScope, onSwitch: (switching: Boolean) -> Unit): Job = scope.launch {
        epoch.collect { current ->
            if (current == seen) return@collect
            seen = current
            onSwitch(current % 2 == 1)
        }
    }
}
