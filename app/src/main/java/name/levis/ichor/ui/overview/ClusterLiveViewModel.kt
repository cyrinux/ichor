package name.levis.ichor.ui.overview

import android.util.Log
import androidx.lifecycle.ViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import name.levis.ichor.BuildConfig
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.model.ClusterStatsSample
import name.levis.ichor.model.ClusterUsage
import name.levis.ichor.model.MergedClusterStats
import name.levis.ichor.model.appendHistory
import name.levis.ichor.model.clusterPollSeconds
import name.levis.ichor.model.clusterUsage
import name.levis.ichor.model.isDenseCluster
import name.levis.ichor.model.liveSampleNodes
import name.levis.ichor.model.mergeClusterStats
import name.levis.ichor.model.usage
import name.levis.ichor.ui.pollEvery

/** The second sample comes sooner, so the CPU shows up (or catches up) about a second after opening. */
private const val FIRST_DELTA_MILLIS = 1_000L

/** CPU history for the sparkline: three minutes, nine on a dense cluster (see clusterPollSeconds). */
const val CLUSTER_HISTORY_POINTS = 36

private const val TAG = "ClusterLive"

/** After this many failed samples in a row, the card falls back to the overview's snapshot. */
private const val MAX_FAILURES = 3

/**
 * Live cluster usage; [usage] is null until the first sample (or after repeated failures).
 * [sampled] of [total] nodes were asked last, on a large cluster between full samples.
 */
data class ClusterLiveState(
    val usage: ClusterUsage? = null,
    val cpuHistory: List<Float> = emptyList(),
    val sampled: Int = 0,
    val total: Int = 0,
) {
    /** Whether the last sample asked only some nodes (the usage still covers all of them). */
    val partial: Boolean get() = sampled in 1 until total
}

/**
 * Polls the nodes' CPU and memory while the overview is visible and live stats are enabled:
 * every node on a small cluster; on a large one the focus set, and every node now and then.
 */
class ClusterLiveViewModel(private val talos: TalosRepository) : ViewModel() {
    private val _state = MutableStateFlow(ClusterLiveState())
    val state: StateFlow<ClusterLiveState> = _state.asStateFlow()

    private var last: ClusterStatsSample? = null
    private var merged: MergedClusterStats? = null
    private var source: Any? = null

    /**
     * Samples until cancelled. History survives leaving the screen and coming back, but not a
     * change of [key] (the cluster, or cached data dropped e.g. when masking changed). Above the
     * dense threshold, a cluster of many [nodes] is sampled less often, and most samples ask only
     * the [focus] nodes (problems and rows on screen), every node every few samples; the total
     * is over every node's last known counters.
     */
    suspend fun poll(key: Any, nodes: Int, focus: StateFlow<Set<String>>) {
        if (key != source) {
            source = key
            clear()
        }
        val dense = isDenseCluster(nodes)
        var failures = 0
        var samples = 0
        var ticks = 0
        pollEvery(
            // Right after (re)starting, a fresh delta needs a second sample: take it soon.
            { if (samples == 1) FIRST_DELTA_MILLIS else clusterPollSeconds(nodes) * 1000 },
            {
                val asked = liveSampleNodes(ticks++, nodes, focus.value)
                if (BuildConfig.DEBUG && dense) Log.d(TAG, "cluster stats: ${if (asked.isEmpty()) "all $nodes" else asked.size} nodes")
                // No node answering a full sample is a failed one, not an empty cluster; a focus
                // set of down nodes answers nothing and keeps the last counters.
                talos.clusterStats(asked).takeIf { it.nodes.isNotEmpty() || asked.isNotEmpty() } ?: error("no node answered")
            },
            {
                // A blip keeps the last values; a cluster that stopped answering must not look live.
                failures++
                if (failures >= MAX_FAILURES) {
                    samples = 0
                    ticks = 0
                    clear()
                }
            },
        ) { sample ->
            val usage = if (dense) denseUsage(sample) else clusterUsage(last, sample)
            val shown = _state.value
            last = sample
            failures = 0
            samples++
            _state.value = ClusterLiveState(
                // One sample without CPU (nodes rebooted or replaced) keeps the last reading.
                usage = usage.copy(cpuFraction = usage.cpuFraction ?: shown.usage?.cpuFraction),
                cpuHistory = appendHistory(shown.cpuHistory, usage.cpuFraction, CLUSTER_HISTORY_POINTS),
                sampled = sample.sampled,
                total = sample.total,
            )
        }
    }

    /** Usage over every node's last known counters, the partial [sample] merged in. */
    private fun denseUsage(sample: ClusterStatsSample): ClusterUsage {
        val next = mergeClusterStats(merged, sample)
        merged = next
        return next.usage()
    }

    /** Forgets the samples, e.g. when live stats are turned off, so a later start is not stale. */
    fun clear() {
        last = null
        merged = null
        _state.value = ClusterLiveState()
    }
}
