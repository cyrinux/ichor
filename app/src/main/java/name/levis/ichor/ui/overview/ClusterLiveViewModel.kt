package name.levis.ichor.ui.overview

import androidx.lifecycle.ViewModel
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.model.ClusterStatsSample
import name.levis.ichor.model.ClusterUsage
import name.levis.ichor.model.appendHistory
import name.levis.ichor.model.clusterPollSeconds
import name.levis.ichor.model.clusterUsage

/** The second sample comes sooner, so the CPU shows up (or catches up) about a second after opening. */
private const val FIRST_DELTA_MILLIS = 1_000L

/** CPU history for the sparkline: three minutes, nine on a dense cluster (see clusterPollSeconds). */
const val CLUSTER_HISTORY_POINTS = 36

/** After this many failed samples in a row, the card falls back to the overview's snapshot. */
private const val MAX_FAILURES = 3

/** Live cluster usage; [usage] is null until the first sample (or after repeated failures). */
data class ClusterLiveState(val usage: ClusterUsage? = null, val cpuHistory: List<Float> = emptyList())

/** Polls every node's CPU and memory while the overview is visible and live stats are enabled. */
class ClusterLiveViewModel(private val talos: TalosRepository) : ViewModel() {
    private val _state = MutableStateFlow(ClusterLiveState())
    val state: StateFlow<ClusterLiveState> = _state.asStateFlow()

    private var last: ClusterStatsSample? = null
    private var source: Any? = null

    /**
     * Samples until cancelled. History survives leaving the screen and coming back, but not a
     * change of [key] (the cluster, or cached data dropped e.g. when masking changed). Each
     * sample asks every node, so a cluster of many [nodes] is sampled less often.
     */
    suspend fun poll(key: Any, nodes: Int) {
        if (key != source) {
            source = key
            clear()
        }
        var failures = 0
        var samples = 0
        while (true) {
            // No node answering in time is a failed sample, not an empty cluster.
            runCatching { talos.clusterStats().takeIf { it.nodes.isNotEmpty() } ?: error("no node answered") }.fold(
                onSuccess = { sample ->
                    val usage = clusterUsage(last, sample)
                    val shown = _state.value
                    last = sample
                    failures = 0
                    samples++
                    _state.value = ClusterLiveState(
                        // One sample without CPU (nodes rebooted or replaced) keeps the last reading.
                        usage = usage.copy(cpuFraction = usage.cpuFraction ?: shown.usage?.cpuFraction),
                        cpuHistory = appendHistory(shown.cpuHistory, usage.cpuFraction, CLUSTER_HISTORY_POINTS),
                    )
                },
                onFailure = {
                    // Leaving the screen cancels the call: that is not a failure.
                    if (it is CancellationException) throw it
                    // A blip keeps the last values; a cluster that stopped answering must not look live.
                    failures++
                    if (failures >= MAX_FAILURES) {
                        samples = 0
                        clear()
                    }
                },
            )
            // Right after (re)starting, a fresh delta needs a second sample: take it soon.
            delay(if (samples == 1) FIRST_DELTA_MILLIS else clusterPollSeconds(nodes) * 1000)
        }
    }

    /** Forgets the samples, e.g. when live stats are turned off, so a later start is not stale. */
    fun clear() {
        last = null
        _state.value = ClusterLiveState()
    }
}
