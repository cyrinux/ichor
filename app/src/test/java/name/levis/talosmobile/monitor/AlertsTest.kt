package name.levis.talosmobile.monitor

import name.levis.talosmobile.model.NodeHealth
import name.levis.talosmobile.model.NodeHealth.NOT_READY
import name.levis.talosmobile.model.NodeHealth.READY
import name.levis.talosmobile.model.NodeHealth.UNREACHABLE
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class AlertsTest {

    private val now = 1_800_000_000_000L
    private val farCert = now / 1000 + 365L * 86_400

    private fun snap(
        vararg nodes: Pair<String, NodeHealth>,
        context: String = "lab",
        alarms: List<String> = emptyList(),
        cert: Long = farCert,
        lastWarn: Long = -1,
    ) = ClusterSnapshot(
        context = context,
        takenAt = now,
        nodes = nodes.associate { (addr, h) -> addr to NodeState("host-$addr", h, if (h == READY) "" else "why") },
        etcdAlarms = alarms,
        etcdChecked = true,
        certNotAfter = cert,
        lastCertWarnDay = lastWarn,
    )

    @Test
    fun firstSnapshotIsSilentBaseline() {
        val result = evaluate(null, snap("a" to UNREACHABLE, "b" to NOT_READY), now)
        assertTrue(result.alerts.isEmpty())
    }

    @Test
    fun alertsOnlyOnTransitions() {
        val prev = snap("a" to READY, "b" to NOT_READY, "c" to READY)
        val cur = snap("a" to UNREACHABLE, "b" to READY, "c" to READY)

        val alerts = evaluate(prev, cur, now).alerts

        assertEquals(listOf("node:a", "node:b"), alerts.map { it.key })
        assertEquals(AlertKind.NODE_UNREACHABLE, alerts[0].kind)
        assertEquals("host-a", alerts[0].subject)
        assertEquals("why", alerts[0].detail)
        assertTrue(alerts[0].problem)
        assertEquals(AlertKind.NODE_READY, alerts[1].kind)
        assertEquals("host-b", alerts[1].subject)
    }

    @Test
    fun steadyStateDoesNotRepeat() {
        val s = snap("a" to NOT_READY)
        assertTrue(evaluate(s, s, now).alerts.isEmpty())
    }

    @Test
    fun contextSwitchIsBaseline() {
        val prev = snap("a" to READY, context = "one")
        val cur = snap("a" to UNREACHABLE, context = "two")
        assertTrue(evaluate(prev, cur, now).alerts.isEmpty())
    }

    @Test
    fun newEtcdAlarmAlertsOnce() {
        val prev = snap("a" to READY)
        val cur = snap("a" to READY, alarms = listOf("beef:NOSPACE"))

        val first = evaluate(prev, cur, now).alerts
        assertEquals(listOf("etcd:beef:NOSPACE"), first.map { it.key })
        assertEquals("NOSPACE", first.single().subject)
        assertEquals("beef", first.single().detail)
        assertTrue(evaluate(cur, cur, now).alerts.isEmpty())
    }

    @Test
    fun certExpiryWarnsOncePerDay() {
        val soon = now / 1000 + 5L * 86_400
        val first = evaluate(snap("a" to READY), snap("a" to READY, cert = soon), now)

        assertEquals(listOf("cert"), first.alerts.map { it.key })
        assertEquals(AlertKind.CERT_EXPIRING, first.alerts.single().kind)
        assertEquals(5, first.alerts.single().days)

        val sameDay = evaluate(first.next, snap("a" to READY, cert = soon), now + 3_600_000)
        assertTrue(sameDay.alerts.isEmpty())

        val nextDay = evaluate(sameDay.next, snap("a" to READY, cert = soon), now + 86_400_000)
        assertEquals(listOf("cert"), nextDay.alerts.map { it.key })
    }

    @Test
    fun expiredCertCountsDaysSince() {
        val past = now / 1000 - 3L * 86_400
        val alert = evaluate(snap("a" to READY), snap("a" to READY, cert = past), now).alerts.single()
        assertEquals(AlertKind.CERT_EXPIRED, alert.kind)
        assertEquals(3, alert.days)
    }

    @Test
    fun certFarAwayNeverWarns() {
        assertTrue(evaluate(null, snap("a" to READY), now).alerts.isEmpty())
    }
}
