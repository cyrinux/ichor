package name.levis.ichor.monitor

import name.levis.ichor.data.TalosJson
import name.levis.ichor.model.FORECAST_HIGH
import name.levis.ichor.model.HistoryMemoryForecast
import name.levis.ichor.model.NodeMemoryForecast
import name.levis.ichor.model.VolumeOutlook
import name.levis.ichor.model.outlook
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class MemoryTrendTest {
    private fun node(daysToCritical: Double?, confident: Boolean = true, slope: Double = 1.8) = NodeMemoryForecast(
        node = "10.0.0.2",
        hostname = "worker-1",
        usedPercent = 84.6,
        slopePerDay = slope,
        daysToCritical = daysToCritical,
        daysToFull = daysToCritical?.plus(8),
        confidence = if (confident) FORECAST_HIGH else "low",
    )

    private fun forecast(vararg nodes: NodeMemoryForecast) = HistoryMemoryForecast(nodes.toList())

    @Test
    fun decodesTheGoShape() {
        val json = """{"nodes":[{"node":"10.0.0.2","hostname":"worker-1","usedPercent":71.2,"latestAt":1780000000000,
            "slopePerDay":1.8,"daysToCritical":6.1,"daysToFull":16,"confidence":"high","points":120,"spanHours":166}]}"""
        val row = TalosJson.decodeFromString(HistoryMemoryForecast.serializer(), json).nodes.single()
        assertEquals("worker-1", row.hostname)
        assertEquals(VolumeOutlook(full = 16, critical = 6), row.outlook())
    }

    @Test
    fun aFlatOrNoisyNodeShowsNoProjection() {
        assertNull(node(null, confident = false, slope = 0.0).outlook())
        assertEquals(VolumeOutlook(growingPerDay = 1.8), node(4.0, confident = false).outlook())
    }

    @Test
    fun opensWithinThreeDaysAndClosesPastSeven() {
        val opened = memoryTrendStep(emptyMap(), forecast(node(2.4)))
        assertEquals(setOf("10.0.0.2"), opened.open.keys)
        val alert = opened.alerts.single()
        assertEquals("memory:10.0.0.2:trend", alert.key)
        assertEquals(AlertKind.MEMORY_TREND, alert.kind)
        assertEquals("worker-1", alert.subject)
        val detail = TrendDetail.parse(alert.detail)
        assertTrue(detail.critical)
        assertEquals(2, detail.days)
        assertEquals(84, detail.percent)

        // Still within the close window: kept, nothing posted again.
        val kept = memoryTrendStep(opened.open, forecast(node(5.0)))
        assertEquals(opened.open.keys, kept.open.keys)
        assertTrue(kept.alerts.isEmpty())

        // Past it: resolved.
        val closed = memoryTrendStep(kept.open, forecast(node(9.0)))
        assertTrue(closed.open.isEmpty())
        assertEquals(AlertKind.MEMORY_TREND_OK, closed.alerts.single().kind)
    }

    @Test
    fun aLowFitNeverOpensAndAMissingRowResolves() {
        assertTrue(memoryTrendStep(emptyMap(), forecast(node(1.0, confident = false))).alerts.isEmpty())
        val opened = memoryTrendStep(emptyMap(), forecast(node(1.0))).open
        val gone = memoryTrendStep(opened, forecast())
        assertEquals(AlertKind.MEMORY_TREND_OK, gone.alerts.single().kind)
    }

    @Test
    fun theAlertGoesThroughTheRulesAsANodeAlert() {
        val alert = memoryTrendStep(emptyMap(), forecast(node(2.0))).alerts.single()
        assertEquals("memory", alert.ruleKind)
        assertEquals("critical", alert.ruleSeverity)
        assertEquals(AlertChannel.NODES, alert.channel)
    }
}
