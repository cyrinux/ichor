package name.levis.ichor.monitor

import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.FORECAST_HIGH
import name.levis.ichor.model.HistoryForecast
import name.levis.ichor.model.NodeHealth
import name.levis.ichor.model.ShareTarget
import name.levis.ichor.model.VolumeForecast
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class StorageTrendTest {
    private val node = "192.0.2.20"
    private val key = "$node|EPHEMERAL"
    private val hostnames = mapOf(node to "worker-1")

    private fun forecast(critical: Double?, full: Double? = critical?.plus(2), confidence: String = FORECAST_HIGH, used: Double = 81.6) =
        HistoryForecast(listOf(VolumeForecast(key, "EPHEMERAL", node, used, 0, 2.4, full, critical, confidence)))

    private fun step(open: Map<String, String>, forecast: HistoryForecast, fill: Set<String> = emptySet()) =
        storageTrendStep(open, forecast, fill, hostnames)

    private fun opened(): Map<String, String> = step(emptyMap(), forecast(2.0)).open

    @Test
    fun opensAtOnceWithinThreeDays() {
        val result = step(emptyMap(), forecast(2.6))
        val alert = result.alerts.single()
        assertEquals("storage:$key:trend", alert.key)
        assertEquals(AlertKind.STORAGE_TREND, alert.kind)
        assertTrue(alert.problem)
        assertEquals("worker-1", alert.subject)
        assertEquals(TrendDetail("worker-1", "EPHEMERAL", critical = true, days = 3, slopePerDay = 2.4, percent = 81), TrendDetail.parse(alert.detail))
        assertEquals(setOf(key), result.open.keys)
    }

    @Test
    fun staysQuietBeyondThreeDays() {
        val result = step(emptyMap(), forecast(5.0))
        assertTrue(result.alerts.isEmpty())
        assertTrue(result.open.isEmpty())
    }

    @Test
    fun anOpenTrendStaysQuietUpToSevenDays() {
        val result = step(opened(), forecast(6.9))
        assertTrue(result.alerts.isEmpty())
        assertEquals(setOf(key), result.open.keys)
        assertEquals(7, TrendDetail.parse(result.open.getValue(key)).days)
    }

    @Test
    fun anOpenTrendDoesNotRepeatWithinThreeDays() {
        assertTrue(step(opened(), forecast(1.0)).alerts.isEmpty())
    }

    @Test
    fun resolvesOncePastSevenDays() {
        val result = step(opened(), forecast(7.5))
        val alert = result.alerts.single()
        assertEquals(AlertKind.STORAGE_TREND_OK, alert.kind)
        assertFalse(alert.problem)
        assertTrue(result.open.isEmpty())
        assertTrue(step(result.open, forecast(7.5)).alerts.isEmpty())
    }

    @Test
    fun resolvesWithoutAProjection() {
        assertEquals(AlertKind.STORAGE_TREND_OK, step(opened(), forecast(null, full = null, confidence = "low")).alerts.single().kind)
        // Already critical: the fill alert takes over, the projection to critical is gone.
        assertEquals(AlertKind.STORAGE_TREND_OK, step(opened(), forecast(null, full = 2.0)).alerts.single().kind)
        assertEquals(AlertKind.STORAGE_TREND_OK, step(opened(), HistoryForecast()).alerts.single().kind)
    }

    @Test
    fun aLowConfidenceProjectionNeverOpens() {
        assertTrue(step(emptyMap(), forecast(1.0, confidence = "low")).alerts.isEmpty())
    }

    @Test
    fun aVolumeInAFillAlertIsLeftToIt() {
        assertTrue(step(emptyMap(), forecast(1.0), fill = setOf(key)).let { it.alerts.isEmpty() && it.open.isEmpty() })
        assertTrue(step(opened(), forecast(1.0), fill = setOf(key)).let { it.alerts.isEmpty() && it.open.isEmpty() })
    }

    @Test
    fun theWordingIsCriticalWhenItComesBeforeFull() {
        val near = trendDetailOf(forecast(2.6, full = 3.2).volumes.single(), "worker-1")
        assertTrue(near.critical)
        assertEquals(3, near.days)
        val full = trendDetailOf(forecast(null, full = 2.4).volumes.single(), "worker-1")
        assertFalse(full.critical)
        assertEquals(2, full.days)
        val critical = trendDetailOf(forecast(0.5, full = 4.0).volumes.single(), "worker-1")
        assertTrue(critical.critical)
        assertEquals(0, critical.days)
    }

    @Test
    fun theDetailSurvivesASeparatorInItsNames() {
        val detail = TrendDetail("worker|1", "EPHEMERAL", critical = false, days = 2, slopePerDay = 1.25, percent = 70)
        assertEquals(detail.copy(hostname = "worker/1"), TrendDetail.parse(detail.format()))
    }

    @Test
    fun theAlertOpensTheNodeStorageWithSnoozeOnly() {
        val alert = step(emptyMap(), forecast(1.0)).alerts.single()
        assertEquals(ShareTarget.storage(node, "worker-1"), alert.shareTarget())
        assertEquals(AlertChannel.NODES, alert.channel)
        assertEquals(listOf(AlertAction.SNOOZE), alert.actions(canWake = true, canReboot = true))
    }

    private val lab = ContextSummary(name = "lab", fingerprint = "fl", clusterId = "lab")

    private fun snap(trends: Map<String, String> = emptyMap(), issues: Map<String, String> = emptyMap(), watched: Boolean = true) = ClusterSnapshot(
        context = "lab",
        takenAt = 0,
        nodes = mapOf(node to NodeState("worker-1", NodeHealth.READY)),
        storageWatched = watched,
        storageChecked = watched,
        storageIssues = issues,
        storageTrends = trends,
    )

    private fun run(before: MonitorState, read: ClusterSnapshot) =
        monitorRun(before, listOf(ClusterRead(lab, read)), 0, unreachableAlerts = false, unreachableRuns = 3, active = "fl")

    @Test
    fun aRunStepsTheTrendsAndAddsTheirAlerts() {
        val before = MonitorState(mapOf("fl" to snap()))
        val reads = listOf(ClusterRead(lab, snap()))
        val result = withStorageTrends(run(before, snap()), before, reads, mapOf("fl" to forecast(1.0)), enabled = true)
        assertEquals(setOf(key), result.state.clusters.getValue("fl").storageTrends.keys)
        assertEquals(listOf(AlertKind.STORAGE_TREND), result.alerts.single().alerts.map { it.kind })
    }

    @Test
    fun aRunWithoutAForecastKeepsTheOpenTrends() {
        val open = opened()
        val before = MonitorState(mapOf("fl" to snap(trends = open)))
        val result = withStorageTrends(run(before, snap()), before, listOf(ClusterRead(lab, snap())), emptyMap(), enabled = true)
        assertEquals(open, result.state.clusters.getValue("fl").storageTrends)
        assertTrue(result.alerts.isEmpty())
    }

    @Test
    fun turningTheTrendsOffForgetsThemQuietly() {
        val before = MonitorState(mapOf("fl" to snap(trends = opened())))
        val result = withStorageTrends(run(before, snap()), before, listOf(ClusterRead(lab, snap())), mapOf("fl" to forecast(9.0)), enabled = false)
        assertTrue(result.state.clusters.getValue("fl").storageTrends.isEmpty())
        assertTrue(result.alerts.isEmpty())
    }

    @Test
    fun anEvaluationKeepsTheOpenTrendsUntilStepped() {
        val open = opened()
        val before = MonitorState(mapOf("fl" to snap(trends = open)))
        assertEquals(open, run(before, snap()).state.clusters.getValue("fl").storageTrends)
    }
}
