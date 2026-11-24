package name.levis.ichor.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.builtins.ListSerializer
import name.levis.ichor.model.AlertDecisions
import name.levis.ichor.model.AlertInput
import name.levis.ichor.model.AlertRules
import name.levis.ichor.model.AlertRulesJson
import name.levis.ichorgo.Ichorgo
import java.util.TimeZone

/**
 * What the alert rules do with a run's alerts, decided by the Go core (Ichorgo.alertRulesApply):
 * no cluster call and no state, like [FleetRepository]. [apply], [quiet] and [zone] are swapped in tests.
 */
class AlertRulesRepository(
    private val apply: (rules: String, alerts: String, now: Long) -> String = { rules, alerts, now -> Ichorgo.alertRulesApply(rules, alerts, now) },
    private val zone: () -> String = { TimeZone.getDefault().id },
    private val quiet: (rules: String, cluster: String, at: Long) -> Long = { rules, cluster, at -> Ichorgo.alertRulesQuietUntil(rules, cluster, at) },
) {
    /**
     * The decisions over [alerts] under [rules] at [now] (unix ms), quiet hours without a zone
     * in the phone's. Throws when Go refuses the rules (a bad stored time or zone): the caller
     * then posts as before the rules.
     */
    suspend fun decide(rules: AlertRules, alerts: List<AlertInput>, now: Long): AlertDecisions = withContext(Dispatchers.Default) {
        val rulesJson = AlertRulesJson.encodeToString(AlertRules.serializer(), rules.withZone(zone()))
        val alertsJson = AlertRulesJson.encodeToString(ListSerializer(AlertInput.serializer()), alerts)
        AlertRulesJson.decodeFromString(AlertDecisions.serializer(), apply(rulesJson, alertsJson, now))
    }

    /**
     * For what is not an alert (a reminder): the end (unix ms) of the quiet hours the cluster
     * [fingerprint] is in at [at] under [rules], 0 outside them, without any, or when Go refuses the rules.
     */
    suspend fun quietUntil(rules: AlertRules, fingerprint: String, at: Long): Long = withContext(Dispatchers.Default) {
        if (rules == AlertRules.NONE) return@withContext 0L
        quiet(AlertRulesJson.encodeToString(AlertRules.serializer(), rules.withZone(zone())), fingerprint, at)
    }
}
