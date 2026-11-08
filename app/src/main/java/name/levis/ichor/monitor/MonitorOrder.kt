package name.levis.ichor.monitor

import name.levis.ichor.model.ContextSummary

/** How many clusters one monitor run reads at a time (see [monitoredOrder]). */
const val MONITOR_CLUSTER_PARALLELISM = 4

/**
 * The order a monitor run reads [contexts] in, [MONITOR_CLUSTER_PARALLELISM] at a time: the ones
 * whose last check failed ([lastFailed], by [monitorKeyOf]) first, so a cluster that stays
 * unreachable alerts as early as before; the [active] one (by name) last while the app is in the
 * [foreground], so the screen's own calls are not raced by the monitor's. Otherwise the order
 * is kept as it was.
 */
fun monitoredOrder(
    contexts: List<ContextSummary>,
    active: String,
    foreground: Boolean,
    lastFailed: Set<String>,
): List<ContextSummary> {
    val (failed, rest) = contexts.partition { monitorKeyOf(it) in lastFailed }
    val ordered = failed + rest
    if (!foreground) return ordered
    val (onScreen, others) = ordered.partition { it.name == active }
    return others + onScreen
}

/** The keys of the clusters whose previous check could not read them, from the stored [state]. */
fun lastFailedKeys(state: MonitorState): Set<String> =
    state.reach.filterValues { it.failures > 0 }.keys
