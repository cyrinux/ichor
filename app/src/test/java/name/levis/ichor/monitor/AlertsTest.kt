package name.levis.ichor.monitor

import name.levis.ichor.model.NodeHealth
import name.levis.ichor.model.NodeHealth.NOT_READY
import name.levis.ichor.model.NodeHealth.READY
import name.levis.ichor.model.NodeHealth.UNREACHABLE
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
    fun wholeClusterUnreachableIsSilentAndKeepsThePreviousSnapshot() {
        val prev = snap("a" to READY, "b" to NOT_READY)
        val away = evaluate(prev, snap("a" to UNREACHABLE, "b" to UNREACHABLE).copy(takenAt = now + 1), now)

        assertTrue(away.alerts.isEmpty())
        assertEquals(prev, away.next)

        // Back on the network: nothing changed since the last real check.
        assertTrue(evaluate(away.next, snap("a" to READY, "b" to NOT_READY), now).alerts.isEmpty())
    }

    @Test
    fun baselineTakenOffNetworkDoesNotAlertOnReturn() {
        val baseline = evaluate(null, snap("a" to UNREACHABLE, "b" to UNREACHABLE), now).next
        val back = evaluate(baseline, snap("a" to READY, "b" to NOT_READY), now)

        assertTrue(back.alerts.isEmpty())
        assertEquals(1, back.next.readyCount)
    }

    @Test
    fun oneNodeOfSeveralUnreachableStillAlerts() {
        val alerts = evaluate(snap("a" to READY, "b" to READY), snap("a" to UNREACHABLE, "b" to READY), now).alerts
        assertEquals(listOf("node:a"), alerts.map { it.key })
    }

    @Test
    fun certStillWarnsWhileOffNetwork() {
        val soon = now / 1000 + 5L * 86_400
        val result = evaluate(snap("a" to READY), snap("a" to UNREACHABLE, cert = soon), now)
        assertEquals(listOf("cert"), result.alerts.map { it.key })
        assertEquals(READY, result.next.nodes.getValue("a").health)
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

    @Test
    fun certAlertStartsOneWeekBeforeExpiry() {
        val in10Days = now / 1000 + 10L * 86_400
        val in7Days = now / 1000 + 7L * 86_400
        assertTrue(evaluate(snap("a" to READY), snap("a" to READY, cert = in10Days), now).alerts.isEmpty())
        assertEquals(listOf("cert"), evaluate(snap("a" to READY), snap("a" to READY, cert = in7Days), now).alerts.map { it.key })
    }
}

class KubeAlertsTest {

    private val now = 1_800_000_000_000L

    private fun kubeSnap(vararg nodes: Pair<String, NodeHealth>, cert: Long, lastWarn: Long = -1) = ClusterSnapshot(
        context = "eks",
        takenAt = now,
        nodes = nodes.associate { (name, h) -> name to NodeState(name, h, if (h == READY) "" else "MemoryPressure") },
        certNotAfter = cert,
        lastCertWarnDay = lastWarn,
        kube = true,
    )

    @Test
    fun expiringKubeconfigCredentialsAreWordedForAKubeconfig() {
        val soon = now / 1000 + 3 * 86_400
        val alerts = evaluate(null, kubeSnap("a" to READY, cert = soon), now).alerts
        assertEquals(listOf("cert"), alerts.map { it.key })
        assertEquals(AlertKind.KUBECONFIG_EXPIRING, alerts.single().kind)
        assertEquals(3, alerts.single().days)
        val expired = evaluate(null, kubeSnap("a" to READY, cert = now / 1000 - 2 * 86_400), now).alerts.single()
        assertEquals(AlertKind.KUBECONFIG_EXPIRED, expired.kind)
        assertEquals(2, expired.days)
    }

    @Test
    fun nodeTransitionsAlertWithoutEtcd() {
        val far = now / 1000 + 365L * 86_400
        val prev = kubeSnap("a" to READY, "b" to NOT_READY, cert = far)
        val cur = kubeSnap("a" to NOT_READY, "b" to READY, cert = far)
        val result = evaluate(prev, cur, now)
        assertEquals(listOf(AlertKind.NODE_NOT_READY, AlertKind.NODE_READY), result.alerts.map { it.kind })
        assertEquals("MemoryPressure", result.alerts[0].detail)
        assertTrue(result.next.kube)
    }
}
