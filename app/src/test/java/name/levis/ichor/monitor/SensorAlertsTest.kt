package name.levis.ichor.monitor

import name.levis.ichor.data.TalosJson
import name.levis.ichor.model.ClusterSensorHealth
import name.levis.ichor.model.NodeHealth
import name.levis.ichor.model.NodeSensorHealth
import name.levis.ichor.model.ShareTarget
import name.levis.ichor.model.TemperatureSensor
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class SensorAlertsTest {
    private val now = 1_800_000_000_000L
    private val farCert = now / 1000 + 365L * 86_400

    private fun worker(celsius: Double, throttled: Boolean = false, crit: Double = 100.0, max: Double = 85.0) = NodeSensorHealth(
        "192.0.2.20", "worker-1",
        hottest = TemperatureSensor("coretemp", "Package id 0", celsius, critCelsius = crit, maxCelsius = max),
        throttled = throttled,
    )

    private fun health(vararg nodes: NodeSensorHealth) = ClusterSensorHealth("lab", nodes.toList())

    private fun snap(issues: Map<String, String>?, watched: Boolean = true) = ClusterSnapshot(
        context = "lab",
        takenAt = now,
        nodes = mapOf("192.0.2.20" to NodeState("worker-1", NodeHealth.READY)),
        etcdChecked = true,
        certNotAfter = farCert,
        sensorWatched = watched,
        sensorChecked = watched && issues != null,
        sensorIssues = issues.orEmpty(),
    )

    @Test
    fun theGoJsonDecodes() {
        val json = """{"context":"lab","nodes":[
            {"node":"192.0.2.20","hostname":"worker-1","hottest":{"chip":"coretemp","label":"Package id 0","celsius":96.0,"critCelsius":100,"maxCelsius":85},"throttled":true},
            {"node":"192.0.2.21","hostname":"vm-1","hottest":null,"throttled":false},
            {"node":"192.0.2.30","hostname":"192.0.2.30","hottest":null,"throttled":false,"error":"connection refused"}]}"""
        val h = TalosJson.decodeFromString(ClusterSensorHealth.serializer(), json)
        assertNull(h.nodes[1].hottest)
        assertEquals(setOf("192.0.2.20|hot", "192.0.2.20|throttled"), sensorIssuesOf(h, SENSOR_THRESHOLD_AUTO).keys)
    }

    @Test
    fun theLimitIsCritMinusTheMarginElseMaxElseTheSetting() {
        val sensor = TemperatureSensor(celsius = 0.0, critCelsius = 100.0, maxCelsius = 85.0)
        assertEquals(95.0, sensorLimit(sensor, SENSOR_THRESHOLD_AUTO)!!, 0.0)
        assertEquals(85.0, sensorLimit(sensor.copy(critCelsius = 0.0), SENSOR_THRESHOLD_AUTO)!!, 0.0)
        assertNull(sensorLimit(TemperatureSensor(celsius = 50.0), SENSOR_THRESHOLD_AUTO))
        assertEquals(70.0, sensorLimit(sensor, 70)!!, 0.0)
        assertEquals(60.0, sensorLimit(sensor, 10)!!, 0.0) // clamped to the range
    }

    @Test
    fun hotNodesAreCriticalAndCoolDownWithHysteresis() {
        assertTrue(sensorIssuesOf(health(worker(94.9)), SENSOR_THRESHOLD_AUTO).isEmpty())
        val hot = sensorIssuesOf(health(worker(95.0)), SENSOR_THRESHOLD_AUTO)
        val detail = SensorDetail.parse(hot.getValue("192.0.2.20|hot"))
        assertEquals(DATA_CRITICAL, detail.severity)
        assertEquals("worker-1", detail.hostname)
        assertEquals("Package id 0", detail.label)
        assertEquals(95.0, detail.limit, 0.0)
        // Under the limit but within the hysteresis: still hot; below it: cleared.
        assertEquals(hot.keys, sensorIssuesOf(health(worker(92.5)), SENSOR_THRESHOLD_AUTO, known = hot).keys)
        assertTrue(sensorIssuesOf(health(worker(92.0)), SENSOR_THRESHOLD_AUTO, known = hot).isEmpty())
    }

    @Test
    fun aNodeThatDidNotAnswerKeepsItsIssues() {
        val known = sensorIssuesOf(health(worker(97.0)), SENSOR_THRESHOLD_AUTO)
        val down = NodeSensorHealth("192.0.2.20", "worker-1", error = "timeout")
        assertEquals(known, sensorIssuesOf(health(down), SENSOR_THRESHOLD_AUTO, known))
    }

    @Test
    fun aHotNodeAlertsAtOnceAndOnceBackToNormal() {
        val first = evaluate(snap(emptyMap()), snap(sensorIssuesOf(health(worker(97.0)), SENSOR_THRESHOLD_AUTO)), now)
        val alert = first.alerts.single()
        assertEquals(AlertKind.SENSOR_PROBLEM, alert.kind)
        assertEquals("worker-1", alert.subject)
        assertEquals(ShareTarget.hardware("192.0.2.20", "worker-1"), alert.shareTarget())
        assertEquals(AlertChannel.NODES, alert.channel)

        val still = evaluate(first.next, snap(sensorIssuesOf(health(worker(98.0)), SENSOR_THRESHOLD_AUTO, first.next.sensorIssues)), now)
        assertTrue(still.alerts.isEmpty())

        val cooled = evaluate(still.next, snap(sensorIssuesOf(health(worker(60.0)), SENSOR_THRESHOLD_AUTO, still.next.sensorIssues)), now)
        assertEquals(listOf(AlertKind.SENSOR_OK), cooled.alerts.map { it.kind })
    }

    @Test
    fun throttlingAlertsOnTheSecondCheckInARow() {
        val throttled = sensorIssuesOf(health(worker(60.0, throttled = true)), SENSOR_THRESHOLD_AUTO)
        val once = evaluate(snap(emptyMap()), snap(throttled), now)
        assertTrue(once.alerts.isEmpty())
        val twice = evaluate(once.next, snap(throttled), now)
        assertEquals(AlertKind.SENSOR_PROBLEM, twice.alerts.single().kind)
        assertTrue(SensorDetail.parse(twice.alerts.single().detail).throttled)
    }

    @Test
    fun notWatchedForgetsTheIssues() {
        val hot = sensorIssuesOf(health(worker(99.0)), SENSOR_THRESHOLD_AUTO)
        val off = evaluate(snap(hot), snap(null, watched = false), now)
        assertTrue(off.alerts.isEmpty())
        assertTrue(off.next.sensorIssues.isEmpty())
    }

    @Test
    fun theDetailRoundTrips() {
        val d = SensorDetail(DATA_CRITICAL, throttled = false, "worker|1", "Package id 0", 96.04, 95.0)
        val back = SensorDetail.parse(d.format())
        assertEquals("worker/1", back.hostname)
        assertEquals(96.0, back.celsius, 0.0)
        assertEquals(95.0, back.limit, 0.0)
    }
}
