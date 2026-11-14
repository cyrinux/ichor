package name.levis.ichor.monitor

import name.levis.ichor.data.TalosJson
import name.levis.ichor.model.ALERT_ACTION_DIGEST
import name.levis.ichor.model.ALERT_ACTION_NOTIFY
import name.levis.ichor.model.ALERT_REASON_FLAPPING
import name.levis.ichor.model.AlertDecision
import name.levis.ichor.model.AlertDecisions
import name.levis.ichor.model.AlertDigest
import name.levis.ichor.model.AlertRule
import name.levis.ichor.model.AlertRules
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.HistoryFlaps
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class AlertFlapsTest {
    private val prod = ContextSummary(name = "prod-eu", fingerprint = "fp-prod", clusterId = "prod")
    private val amWarn = Alert("am:abc", AlertKind.AM_FIRING, true, "KubePodCrashLooping", "warning|KubePodCrashLooping|shop/web-0")
    private val data = Alert("data:longhorn|vol-1", AlertKind.DATA_PROBLEM, true, "vol-1", "longhorn|warning")

    @Test
    fun theRuleOfAClusterOverridesTheDefault() {
        val rules = AlertRules(default = AlertRule(flapOpens = 3), clusters = mapOf("fp-lab" to AlertRule(flapOpens = 0), "fp-qa" to AlertRule(digest = true)))
        assertEquals(3, rules.flapOpens("fp-prod"))
        assertEquals(0, rules.flapOpens("fp-lab"))
        assertEquals(3, rules.flapOpens("fp-qa"))
        assertEquals(0, AlertRules.NONE.flapOpens("fp-prod"))
        assertTrue(AlertRule(flapOpens = 0).isEmpty.not())
    }

    @Test
    fun inputsCarryTheOpensOfTheirClusterAndKey() {
        val clusters = listOf(ClusterAlerts(prod, listOf(amWarn, data)))
        val flaps = mapOf("fp-prod" to mapOf(amWarn.key to 4), "fp-other" to mapOf(data.key to 9))
        val inputs = alertInputs(clusters, flaps) { _, _ -> false }
        assertEquals(listOf(4, 0), inputs.map { it.flaps })
    }

    @Test
    fun theDigestNamesTheFlappingAlerts() {
        val clusters = listOf(ClusterAlerts(prod, listOf(amWarn, data)))
        val inputs = alertInputs(clusters) { _, _ -> false }
        val digest = AlertDigest("fp-prod", count = 1, warning = 1, firstKeys = listOf(amWarn.key), flapping = 1, flappingKeys = listOf(amWarn.key))
        val decisions = AlertDecisions(
            decisions = listOf(
                AlertDecision("fp-prod", amWarn.key, ALERT_ACTION_DIGEST, ALERT_REASON_FLAPPING, flaps = 4),
                AlertDecision("fp-prod", data.key, ALERT_ACTION_NOTIFY),
            ),
            digests = listOf(digest),
        )
        val plan = alertPlan(clusters, inputs, decisions)
        assertEquals(listOf(ClusterAlerts(prod, listOf(data))), plan.notify)
        assertEquals(listOf(DigestPost(prod, digest, amWarn.channel, flapping = listOf("KubePodCrashLooping"))), plan.digests)
    }

    @Test
    fun goAnswersDecode() {
        val flaps = TalosJson.decodeFromString(
            HistoryFlaps.serializer(),
            """{"windowHours":24,"alerts":[{"key":"am:abc","opens":5,"closes":4,"open":true,"lastOpenedAt":1780000000000}]}""",
        )
        assertEquals(mapOf("am:abc" to 5), flaps.opensByKey)
        val decisions = TalosJson.decodeFromString(
            AlertDecisions.serializer(),
            """{"decisions":[{"cluster":"fp","key":"am:abc","action":"digest","reason":"flapping","until":0,"repeats":false,"flaps":5}],
               "digests":[{"cluster":"fp","count":1,"critical":0,"warning":1,"firstKeys":["am:abc"],"flapping":1,"flappingKeys":["am:abc"]}]}""",
        )
        assertEquals(5, decisions.decisions.single().flaps)
        assertEquals(listOf("am:abc"), decisions.digests.single().flappingKeys)
    }
}
