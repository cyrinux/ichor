package name.levis.ichor.monitor

import kotlinx.serialization.Serializable
import name.levis.ichor.model.ConfigSummary
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.isDemo

/** Which cluster [context] is of: its cluster id, else (none known) the context on its own. */
fun clusterKeyOf(context: ContextSummary): String = context.clusterId.ifBlank { context.fingerprint.ifBlank { context.name } }

/** The fingerprints of every context of [context]'s cluster in [summary]: a per-cluster setting covers them all. */
fun clusterFingerprints(summary: ConfigSummary, context: ContextSummary): List<String> {
    val key = clusterKeyOf(context)
    return summary.contexts.filter { clusterKeyOf(it) == key }.map { it.fingerprint }.filter { it.isNotBlank() }
}

/**
 * The contexts a background check reads: one per cluster, the [active] one when it is of that
 * cluster, else its first. A cluster any context of which is [unwatched] (by fingerprint) is
 * left out, and so is the demo unless it is on screen.
 */
fun monitoredContexts(summary: ConfigSummary, active: String, unwatched: Set<String>): List<ContextSummary> =
    summary.contexts.groupBy(::clusterKeyOf).values.mapNotNull { group ->
        if (group.any { it.fingerprint.isNotBlank() && it.fingerprint in unwatched }) return@mapNotNull null
        val chosen = group.firstOrNull { it.name == active } ?: group.first()
        chosen.takeUnless { it.isDemo && it.name != active }
    }

/** The key of the "cluster unreachable" alert (one per cluster). */
const val UNREACHABLE_KEY = "unreachable"

/** How many checks in a row a cluster must fail before "cluster unreachable" alerts: the default, and the range offered. */
const val UNREACHABLE_RUNS_DEFAULT = 3
val UNREACHABLE_RUNS = 2..10

/** A cluster's checks that could not read it, in a row, and whether "unreachable" was posted for them. */
@Serializable
data class Reach(val failures: Int = 0, val notified: Boolean = false)

data class ReachStep(val next: Reach, val alert: Alert?)

/**
 * One check of a cluster that answered ([reachable]) or not (no node answered, the API did not, or
 * the check timed out). The [runs]-th failure in a row alerts once, when [enabled]; the first
 * answer after that says it is back, once; an answer resets the count.
 */
fun reachStep(prev: Reach?, reachable: Boolean, runs: Int, enabled: Boolean): ReachStep {
    val before = prev ?: Reach()
    if (reachable) {
        val back = Alert(UNREACHABLE_KEY, AlertKind.CLUSTER_REACHABLE, problem = false).takeIf { before.notified }
        return ReachStep(Reach(), back)
    }
    val failures = (before.failures + 1).coerceAtMost(MAX_FAILURES)
    val due = enabled && !before.notified && failures >= runs
    val alert = if (due) Alert(UNREACHABLE_KEY, AlertKind.CLUSTER_UNREACHABLE, problem = true, detail = failures.toString()) else null
    return ReachStep(Reach(failures, before.notified || due), alert)
}

/** Enough to say "for a long time" without growing for ever. */
private const val MAX_FAILURES = 1_000
