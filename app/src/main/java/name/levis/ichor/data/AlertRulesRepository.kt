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
 * no cluster call and no state, like [FleetRepository]. [apply] and [zone] are swapped in tests.
 */
class AlertRulesRepository(
    private val apply: (rules: String, alerts: String, now: Long) -> String = { rules, alerts, now -> Ichorgo.alertRulesApply(rules, alerts, now) },
    private val zone: () -> String = { TimeZone.getDefault().id },
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
}
