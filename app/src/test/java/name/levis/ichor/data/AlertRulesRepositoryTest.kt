package name.levis.ichor.data

import kotlinx.coroutines.runBlocking
import name.levis.ichor.model.ALERT_ACTION_NOTIFY
import name.levis.ichor.model.AlertInput
import name.levis.ichor.model.AlertRule
import name.levis.ichor.model.AlertRules
import name.levis.ichor.model.QuietHours
import org.junit.Assert.assertEquals
import org.junit.Assert.assertThrows
import org.junit.Test

class AlertRulesRepositoryTest {
    private val alerts = listOf(
        AlertInput(cluster = "fp", key = "node:10.0.0.2", kind = "node", problem = true),
        AlertInput(cluster = "fp", key = "am:x", kind = "am", severity = "warning", problem = true, snoozed = true),
    )

    @Test
    fun goGetsThePhoneZoneAndTheAlertsAsItReadsThem() = runBlocking {
        var sent: Triple<String, String, Long>? = null
        val repository = AlertRulesRepository(
            apply = { rules, input, now ->
                sent = Triple(rules, input, now)
                """{"decisions":[{"cluster":"fp","key":"node:10.0.0.2","action":"notify"},{"cluster":"fp","key":"am:x","action":"mute","reason":"snoozed"}],"digests":[]}"""
            },
            zone = { "Europe/Paris" },
        )
        val rules = AlertRules(default = AlertRule(quietHours = QuietHours("23:00", "07:00", exceptCritical = true)))
        val decisions = repository.decide(rules, alerts, now = 42)
        assertEquals(
            Triple(
                """{"default":{"quietHours":{"start":"23:00","end":"07:00","tz":"Europe/Paris","exceptCritical":true}}}""",
                """[{"cluster":"fp","key":"node:10.0.0.2","kind":"node","problem":true},""" +
                    """{"cluster":"fp","key":"am:x","kind":"am","severity":"warning","problem":true,"snoozed":true}]""",
                42L,
            ),
            sent,
        )
        assertEquals(listOf(ALERT_ACTION_NOTIFY, "mute"), decisions.decisions.map { it.action })
    }

    @Test
    fun aRulesErrorIsThrownForTheCallerToFallBack() {
        val repository = AlertRulesRepository(apply = { _, _, _ -> throw IllegalStateException("alert rule default: quiet hours: start \"7\": want HH:MM") }, zone = { "UTC" })
        assertThrows(IllegalStateException::class.java) { runBlocking { repository.decide(AlertRules.NONE, alerts, 0) } }
    }
}
