package name.levis.ichor.monitor

import name.levis.ichor.model.ALERT_ACTION_REPEAT
import name.levis.ichor.model.AlertDecision
import name.levis.ichor.model.AlertInput
import name.levis.ichor.model.ContextSummary

// Repeat until acknowledged: a critical alert that still fires is posted again every
// repeatMinutes of its rule (Go decides, see AlertRulesApply), until it is acknowledged from its
// notification, snoozed, or it recovers. The snapshot keeps when each firing alert was first
// seen and last posted; the cadence is the check's.

/**
 * [next] with the first-seen and last-posted times of its firing alerts (see [firingAlerts])
 * carried over from [prev], the cluster's previous snapshot: a newly firing one is first seen
 * [now], one that no longer fires is forgotten (so it repeats again only once posted again).
 * Another context's previous snapshot is a baseline.
 */
fun withAlertTimes(prev: ClusterSnapshot?, next: ClusterSnapshot, now: Long): ClusterSnapshot {
    val known = prev?.takeIf { it.context == next.context }
    val firing = firingAlerts(next).map { it.key }.toSet()
    return next.copy(
        firstSeen = firing.associateWith { known?.firstSeen?.get(it) ?: now },
        lastNotified = known?.lastNotified.orEmpty().filterKeys { it in firing },
    )
}

/**
 * The alerts of the clusters read this run that still fire, unchanged, since they were posted:
 * what Go may post again. Not the ones [changed] already lists (posted as they are now), nor a
 * cluster that could not be read or did not answer at all.
 */
fun ongoingAlerts(reads: List<ClusterRead>, state: MonitorState, changed: List<ClusterAlerts>): List<ClusterAlerts> =
    reads.mapNotNull { read ->
        if (read.skipped || read.snapshot == null || read.snapshot.unreachableAsAWhole) return@mapNotNull null
        val key = monitorKeyOf(read.context)
        val snapshot = state.clusters[key] ?: return@mapNotNull null
        val posted = changed.filter { monitorKeyOf(it.context) == key }.flatMap { it.alerts }.map { it.key }.toSet()
        firingAlerts(snapshot)
            .filter { it.key in snapshot.lastNotified && it.key !in posted }
            .takeIf { it.isNotEmpty() }
            ?.let { ClusterAlerts(read.context, it) }
    }

/**
 * The keys of the alerts still firing on each cluster read this run, by fingerprint: an
 * acknowledgement of any other key is over (see [AlertAcks.retainFiring]). A cluster that could
 * not be read keeps its acknowledgements.
 */
fun firingKeys(reads: List<ClusterRead>, state: MonitorState): Map<String, Set<String>> =
    reads.filter { !it.skipped && it.snapshot != null && it.context.fingerprint.isNotBlank() }
        .associate { read ->
            read.context.fingerprint to state.clusters[monitorKeyOf(read.context)]?.let(::firingAlerts).orEmpty().map { it.key }.toSet()
        }

/** [ongoing]'s alerts as Go reads them, with their times from [state]; [snoozed] and [acknowledged] by cluster (fingerprint) and key. */
fun repeatInputs(
    ongoing: List<ClusterAlerts>,
    state: MonitorState,
    snoozed: (cluster: String, key: String) -> Boolean,
    acknowledged: (cluster: String, key: String) -> Boolean,
): List<AlertInput> = ongoing.flatMap { group ->
    val fingerprint = group.context.fingerprint
    val snapshot = state.clusters[monitorKeyOf(group.context)]
    group.alerts.map { alert ->
        AlertInput(
            cluster = fingerprint,
            key = alert.key,
            kind = alert.ruleKind,
            severity = alert.ruleSeverity,
            problem = true,
            snoozed = snoozed(fingerprint, alert.key),
            ongoing = true,
            firstSeenMillis = snapshot?.firstSeen?.get(alert.key) ?: 0,
            lastNotifiedMillis = snapshot?.lastNotified?.get(alert.key) ?: 0,
            acknowledged = acknowledged(fingerprint, alert.key),
        )
    }
}

/** An alert posted again: [minutes] since it was first seen. */
data class RepeatPost(val context: ContextSummary, val alert: Alert, val minutes: Long)

/**
 * The [ongoing] alerts Go said to post again, from its [decisions] over [inputs] (see
 * [repeatInputs], same order). Without decisions nothing repeats: a repeat is never a fallback.
 */
fun repeatPlan(ongoing: List<ClusterAlerts>, inputs: List<AlertInput>, decisions: List<AlertDecision>?, now: Long): List<RepeatPost> {
    if (decisions == null) return emptyList()
    var index = 0
    return ongoing.flatMap { group ->
        group.alerts.mapNotNull { alert ->
            val input = inputs.getOrNull(index)
            val decision = decisions.getOrNull(index)
            index++
            val repeat = input != null && decision != null && decision.cluster == input.cluster && decision.key == input.key &&
                decision.action == ALERT_ACTION_REPEAT
            if (!repeat) return@mapNotNull null
            RepeatPost(group.context, alert, stillFiringMinutes(input.firstSeenMillis, now))
        }
    }
}

/** Whole minutes from [firstSeen] to [now], at least 1. */
fun stillFiringMinutes(firstSeen: Long, now: Long): Long = if (firstSeen <= 0) 1 else ((now - firstSeen) / 60_000L).coerceAtLeast(1)

/** [state] with the problem alerts of [posted] posted at [now]: what their next repeat counts from. Only the ones still firing. */
fun withNotified(state: MonitorState, posted: List<ClusterAlerts>, now: Long): MonitorState {
    val byCluster = posted.groupBy { monitorKeyOf(it.context) }
    if (byCluster.isEmpty()) return state
    val clusters = state.clusters.mapValues { (key, snapshot) ->
        val keys = byCluster[key].orEmpty().flatMap { it.alerts }.filter { it.problem && it.key in snapshot.firstSeen }.map { it.key }
        if (keys.isEmpty()) snapshot else snapshot.copy(lastNotified = snapshot.lastNotified + keys.associateWith { now })
    }
    return state.copy(clusters = clusters)
}
