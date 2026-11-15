package name.levis.ichor.monitor

import name.levis.ichor.data.TalosJson
import name.levis.ichor.model.RelayAlert
import name.levis.ichor.model.RelayPayload
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class RelayPayloadTest {

    /** What RelayDecodePayload returns (relay_payload.go), trimmed. */
    private val json = """
        {"cluster":"fp1","sentAt":1791540000000,"truncated":true,"omitted":3,"alerts":[
          {"fingerprint":"a1","alertname":"NodeDown","severity":"critical","status":"firing","namespace":"monitoring","startsAt":1791539000000},
          {"fingerprint":"b2","alertname":"PodCrash|Loop","severity":"warning","status":"firing","namespace":"shop","summary":"Restarting"},
          {"fingerprint":"c3","alertname":"Watchdog","severity":"info","status":"firing"},
          {"fingerprint":"d4","alertname":"DiskFull","severity":"other","status":"resolved"},
          {"fingerprint":"","alertname":"NoKey","severity":"critical","status":"firing"}]}
    """.trimIndent()

    private val payload = TalosJson.decodeFromString(RelayPayload.serializer(), json)

    @Test
    fun decodesTheGoShape() {
        assertEquals("fp1", payload.cluster)
        assertEquals(5, payload.alerts.size)
        assertEquals("Restarting", payload.alerts[1].summary)
        assertTrue(payload.alerts[0].firing)
    }

    @Test
    fun alertsAreKeyedAndWordedLikeThePollers() {
        val alerts = relayAlerts(payload)
        assertEquals(listOf("am:a1", "am:b2", "am:d4"), alerts.map { it.key })
        assertEquals(Alert("am:a1", AlertKind.AM_FIRING, true, "NodeDown", "critical|NodeDown|monitoring"), alerts[0])
        // The separator never leaks into a part, so the detail always splits back the same way.
        assertEquals(AmDetail("warning", "PodCrash/Loop", "shop"), AmDetail.parse(alerts[1].detail))
    }

    @Test
    fun aResolvedAlertReplacesItsFiringNotification() {
        val firing = relayAlerts(RelayPayload("fp1", listOf(RelayAlert("d4", "DiskFull", "critical", "firing")))).single()
        val resolved = relayAlerts(payload).single { it.key == "am:d4" }
        assertEquals(firing.key, resolved.key)
        assertEquals(AlertKind.AM_RESOLVED, resolved.kind)
        assertEquals(false, resolved.problem)
        assertEquals("warning", AmDetail.parse(resolved.detail).severity)
    }

    @Test
    fun omittedCountsOnlyWhenTruncated() {
        assertEquals(3, relayOmitted(payload))
        assertEquals(0, relayOmitted(payload.copy(truncated = false)))
        assertEquals(0, relayOmitted(payload.copy(omitted = -1)))
    }
}
