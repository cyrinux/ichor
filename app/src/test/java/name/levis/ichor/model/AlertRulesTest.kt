package name.levis.ichor.model

import java.io.File
import kotlinx.serialization.SerializationException
import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test

class AlertRulesTest {
    private val night = QuietHours(start = "23:00", end = "07:00", tz = "", exceptCritical = true)
    private val rules = AlertRules(
        default = AlertRule(quietHours = night, minSeverity = ALERT_SEVERITY_WARNING),
        clusters = mapOf(
            "fp-staging" to AlertRule(digest = true, minSeverity = ALERT_SEVERITY_ALL),
            "fp-lab" to AlertRule(quietHours = QuietHours.OFF, minSeverity = ALERT_SEVERITY_CRITICAL),
        ),
    )

    @Test
    fun theRulesRoundTrip() {
        val json = AlertRulesJson.encodeToString(AlertRules.serializer(), rules)
        assertEquals(rules, AlertRulesJson.decodeFromString(AlertRules.serializer(), json))
    }

    @Test
    fun anAbsentFieldIsLeftOutForGoToReadAsTheDefault() {
        val json = AlertRulesJson.encodeToString(AlertRules.serializer(), AlertRules(clusters = mapOf("fp" to AlertRule(digest = true))))
        assertEquals("""{"clusters":{"fp":{"digest":true}}}""", json)
        assertEquals("{}", AlertRulesJson.encodeToString(AlertRules.serializer(), AlertRules.NONE))
    }

    @Test
    fun theSampleRulesGoShipsDecode() {
        val dir = listOf("../go/cmd/probe/testdata", "go/cmd/probe/testdata").map(::File).first { it.isDirectory }
        val sample = AlertRulesJson.decodeFromString(AlertRules.serializer(), File(dir, "alert-rules.json").readText())
        assertEquals("Europe/Paris", sample.default.quietHours?.tz)
        assertEquals(true, sample.clusters.getValue("0f1e2d3c4b5a6978").digest)
        assertTrue(sample.clusters.getValue("9988776655443322").quietHours?.isOff == true)
    }

    @Test
    fun anUnknownKeyFromGoIsAnError() {
        assertThrows(SerializationException::class.java) {
            AlertRulesJson.decodeFromString(AlertDecisions.serializer(), """{"decisions":[],"digests":[],"extra":1}""")
        }
    }

    @Test
    fun goAnswerDecodes() {
        val answer = """{"decisions":[{"cluster":"fp","key":"am:x","action":"mute","reason":"quiet","until":1700000000000}],
            "digests":[{"cluster":"fp","count":3,"critical":1,"warning":2,"firstKeys":["am:x"]}]}"""
        val decoded = AlertRulesJson.decodeFromString(AlertDecisions.serializer(), answer)
        assertEquals(AlertDecision("fp", "am:x", ALERT_ACTION_MUTE, ALERT_REASON_QUIET, 1_700_000_000_000), decoded.decisions.single())
        assertEquals(AlertDigest("fp", 3, 1, 2, listOf("am:x")), decoded.digests.single())
    }

    @Test
    fun thePhoneZoneFillsTheWindowsWithoutOne() {
        val zoned = rules.copy(clusters = rules.clusters + ("fp-ny" to AlertRule(quietHours = night.copy(tz = "America/New_York")))).withZone("Europe/Paris")
        assertEquals("Europe/Paris", zoned.default.quietHours?.tz)
        assertEquals("America/New_York", zoned.clusters.getValue("fp-ny").quietHours?.tz)
        assertEquals("Europe/Paris", zoned.clusters.getValue("fp-lab").quietHours?.tz)
        assertNull(zoned.clusters.getValue("fp-staging").quietHours)
    }

    @Test
    fun aClusterOverrideIsSetDroppedWhenEmptyAndRetained() {
        val set = AlertRules.NONE.withCluster("fp", AlertRule(minSeverity = ALERT_SEVERITY_CRITICAL))
        assertEquals(AlertRule(minSeverity = ALERT_SEVERITY_CRITICAL), set.clusterRule("fp"))
        assertEquals(AlertRules.NONE, set.withCluster("fp", AlertRule()))
        assertEquals(setOf("fp-lab"), rules.retain(listOf("fp-lab", "fp-other")).clusters.keys)
    }

    @Test
    fun aBackupCarriesTheRulesOfItsClustersOnly() {
        assertNull(backupAlertRules(AlertRules.NONE, listOf("fp")))
        assertEquals(setOf("fp-staging"), backupAlertRules(rules, listOf("fp-staging"))?.clusters?.keys)
        val payload = BackupPayload(settings = BackupSettings(alertRules = rules))
        val read = TalosJson.decodeFromString(BackupPayload.serializer(), TalosJson.encodeToString(BackupPayload.serializer(), payload))
        assertEquals(rules, read.settings.alertRules)
        // An older backup has none: the device keeps its own.
        assertNull(TalosJson.decodeFromString(BackupPayload.serializer(), """{"format":3,"talosconfig":""}""").settings.alertRules)
    }
}
