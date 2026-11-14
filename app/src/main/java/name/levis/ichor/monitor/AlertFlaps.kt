package name.levis.ichor.monitor

import name.levis.ichor.TalosApp
import name.levis.ichor.model.AlertRules

/** The opens per day from which [fingerprint]'s alerts are flapping (its override, else the default); 0 off. */
fun AlertRules.flapOpens(fingerprint: String): Int = (clusterRule(fingerprint).flapOpens ?: default.flapOpens) ?: 0

/**
 * The opens in the last 24 h of each alert key, by cluster fingerprint, for the clusters of
 * [clusters] whose rule folds flapping alerts: read from the history ring, which already holds
 * this run's record (appended before the alerts are decided), so the open that fires now counts.
 * Nodes, KubeSpan and sensors are not in the ring's alerts: they never get a count.
 */
suspend fun flapCounts(app: TalosApp, clusters: List<ClusterAlerts>, rules: AlertRules, now: Long): Map<String, Map<String, Int>> =
    clusters.map { it.context.fingerprint }
        .distinct()
        .filter { it.isNotBlank() && rules.flapOpens(it) > 0 }
        .associateWith { fingerprint -> app.historyRepository.flaps(fingerprint, now)?.opensByKey.orEmpty() }
        .filterValues { it.isNotEmpty() }
