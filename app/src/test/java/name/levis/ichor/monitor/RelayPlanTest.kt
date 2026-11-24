package name.levis.ichor.monitor

import name.levis.ichor.model.ALERT_ACTION_DIGEST
import name.levis.ichor.model.ALERT_ACTION_MUTE
import name.levis.ichor.model.ALERT_ACTION_NOTIFY
import name.levis.ichor.model.ALERT_REASON_DIGEST
import name.levis.ichor.model.ALERT_REASON_QUIET
import name.levis.ichor.model.ALERT_REASON_SEVERITY
import name.levis.ichor.model.ALERT_REASON_SNOOZED
import name.levis.ichor.model.AlertDecision
import name.levis.ichor.model.AlertDecisions
import name.levis.ichor.model.AlertInput
import org.junit.Assert.assertEquals
import org.junit.Test

class RelayPlanTest {
    private val fingerprint = "fp-prod"
    private val warning = Alert("am:abc", AlertKind.AM_FIRING, true, "KubePodCrashLooping", "warning|KubePodCrashLooping|shop")
    private val critical = Alert("am:def", AlertKind.AM_FIRING, true, "NodeDown", "critical|NodeDown|")
    private val resolved = Alert("am:ghi", AlertKind.AM_RESOLVED, false, "DiskFull", "warning|DiskFull|shop")
    private val alerts = listOf(warning, critical, resolved)

    private fun inputs(snoozed: Set<String> = emptySet()) = relayInputs(fingerprint, alerts) { it in snoozed }

    private fun decision(alert: Alert, action: String, reason: String = "", until: Long = 0) =
        AlertDecision(cluster = fingerprint, key = alert.key, action = action, reason = reason, until = until)

    @Test
    fun theInputsAreWhatGoReadsOfAPushedAlert() {
        assertEquals(
            listOf(
                AlertInput(cluster = fingerprint, key = "am:abc", kind = "am", severity = "warning", problem = true),
                AlertInput(cluster = fingerprint, key = "am:def", kind = "am", severity = "critical", problem = true, snoozed = true),
                AlertInput(cluster = fingerprint, key = "am:ghi", kind = "am", severity = "warning", problem = false),
            ),
            inputs(snoozed = setOf("am:def")),
        )
    }

    @Test
    fun withoutDecisionsEveryAlertNotSnoozedIsPosted() {
        assertEquals(RelayPlan(listOf(warning, resolved)), relayPlan(alerts, inputs(snoozed = setOf("am:def")), null))
    }

    @Test
    fun quietHoursMuteAndRecordWhileACriticalAlertGetsThrough() {
        val decisions = AlertDecisions(
            listOf(
                decision(warning, ALERT_ACTION_MUTE, ALERT_REASON_QUIET, until = 700),
                decision(critical, ALERT_ACTION_NOTIFY),
                decision(resolved, ALERT_ACTION_MUTE, ALERT_REASON_QUIET, until = 700),
            ),
        )
        assertEquals(
            RelayPlan(
                post = listOf(critical),
                muted = listOf(RelayMuted(warning, ALERT_REASON_QUIET, 700), RelayMuted(resolved, ALERT_REASON_QUIET, 700)),
            ),
            relayPlan(alerts, inputs(), decisions),
        )
    }

    @Test
    fun theMinimumSeverityIsRecordedASnoozeIsNotAndADigestIsPostedOnItsOwn() {
        val decisions = AlertDecisions(
            listOf(
                decision(warning, ALERT_ACTION_MUTE, ALERT_REASON_SEVERITY),
                decision(critical, ALERT_ACTION_MUTE, ALERT_REASON_SNOOZED),
                decision(resolved, ALERT_ACTION_DIGEST, ALERT_REASON_DIGEST),
            ),
        )
        assertEquals(
            RelayPlan(post = listOf(resolved), muted = listOf(RelayMuted(warning, ALERT_REASON_SEVERITY, 0))),
            relayPlan(alerts, inputs(snoozed = setOf("am:def")), decisions),
        )
    }

    @Test
    fun aDecisionForAnotherAlertIsNotTrusted() {
        val decisions = AlertDecisions(listOf(decision(critical, ALERT_ACTION_MUTE, ALERT_REASON_QUIET, until = 700)))
        assertEquals(RelayPlan(alerts), relayPlan(alerts, inputs(), decisions))
    }
}
