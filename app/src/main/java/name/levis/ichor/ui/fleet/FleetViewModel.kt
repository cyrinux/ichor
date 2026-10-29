package name.levis.ichor.ui.fleet

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.flow
import kotlinx.coroutines.flow.mapLatest
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import kotlinx.coroutines.withTimeoutOrNull
import name.levis.ichor.data.FleetRepository
import name.levis.ichor.data.FleetSources
import name.levis.ichor.model.FleetSummary

/** The fleet screen: [summary] once ranked (null before), [now] the time its ages are drawn at, [failed] when the core refused the input. */
data class FleetState(val summary: FleetSummary? = null, val now: Long = 0, val failed: Boolean = false)

/**
 * The fleet screen's list, recomputed whenever [sources] change (a background check landed, a
 * cluster was renamed or added, screenshot mode) and once a minute, so the ages and the "not
 * checked lately" tone follow the clock. Opening it calls no cluster; [refresh] asks the
 * monitor for one check now ([runCheck]) and shows progress until its snapshots change.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class FleetViewModel(
    private val fleet: FleetRepository,
    private val sources: Flow<FleetSources>,
    private val runCheck: suspend () -> Unit,
    private val clock: () -> Long = System::currentTimeMillis,
    private val tickMillis: Long = TICK_MS,
    private val refreshTimeoutMillis: Long = REFRESH_TIMEOUT_MS,
) : ViewModel() {
    private val ticks = flow {
        while (true) {
            emit(Unit)
            delay(tickMillis)
        }
    }

    val state: StateFlow<FleetState> = combine(sources, ticks) { s, _ -> s }
        .mapLatest { s ->
            val now = clock()
            try {
                FleetState(fleet.summary(s, now), now)
            } catch (e: CancellationException) {
                throw e
            } catch (_: Exception) {
                FleetState(now = now, failed = true)
            }
        }
        .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), FleetState())

    private val _refreshing = MutableStateFlow(false)

    /** A check asked for with [refresh] has not landed yet. */
    val refreshing: StateFlow<Boolean> = _refreshing.asStateFlow()

    /** Runs one background check now; [refreshing] until the snapshots change, or a timeout. */
    fun refresh() {
        if (_refreshing.value) return
        _refreshing.value = true
        viewModelScope.launch {
            try {
                val before = sources.first().state
                runCheck()
                withTimeoutOrNull(refreshTimeoutMillis) { sources.first { it.state != before } }
            } finally {
                _refreshing.value = false
            }
        }
    }

    private companion object {
        const val TICK_MS = 60_000L

        /** A check of every cluster can take a while (unreachable ones time out). */
        const val REFRESH_TIMEOUT_MS = 120_000L
    }
}
