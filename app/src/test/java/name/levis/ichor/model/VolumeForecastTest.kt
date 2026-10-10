package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class VolumeForecastTest {
    private fun volume(
        slope: Double = 2.0,
        full: Double? = 22.0,
        critical: Double? = 19.5,
        confidence: String = FORECAST_HIGH,
    ) = VolumeForecast("192.0.2.20|EPHEMERAL", "EPHEMERAL", "192.0.2.20", 56.0, 0, slope, full, critical, confidence)

    @Test
    fun theGoJsonDecodes() {
        val json = """{"volumes":[
            {"key":"192.0.2.20|EPHEMERAL","name":"EPHEMERAL","node":"192.0.2.20","usedPercent":56,"latestAt":1780000000000,
             "slopePerDay":2,"daysToFull":22,"daysToCritical":19.5,"confidence":"high","points":37,"spanHours":72},
            {"key":"192.0.2.20|STATE","name":"STATE","node":"192.0.2.20","usedPercent":40,"latestAt":1780000000000,
             "slopePerDay":0,"confidence":"low","points":37,"spanHours":72}]}"""
        val forecast = TalosJson.decodeFromString(HistoryForecast.serializer(), json)
        val (ephemeral, state) = forecast.volumes
        assertEquals(19.5, ephemeral.daysToCritical!!, 0.0)
        assertEquals(22.0, ephemeral.daysToFull!!, 0.0)
        assertEquals(true, ephemeral.confident)
        assertEquals(37, ephemeral.points)
        assertNull(state.daysToFull)
        assertNull(state.daysToCritical)
        assertEquals(false, state.confident)
    }

    @Test
    fun anEmptyForecastDecodes() {
        assertEquals(emptyList<VolumeForecast>(), TalosJson.decodeFromString(HistoryForecast.serializer(), """{"volumes":[]}""").volumes)
    }

    @Test
    fun daysRoundAndBelowOneReadsAsLessThanADay() {
        assertEquals(0, forecastDays(0.0))
        assertEquals(0, forecastDays(0.9))
        assertEquals(1, forecastDays(1.4))
        assertEquals(3, forecastDays(2.5))
        assertEquals(22, forecastDays(22.0))
    }

    @Test
    fun criticalShowsWhenItComesBeforeFull() {
        assertEquals(VolumeOutlook(full = 22, critical = 20), volume().outlook())
    }

    @Test
    fun criticalRoundingToFullShowsFullOnly() {
        assertEquals(VolumeOutlook(full = 4, critical = null), volume(full = 4.2, critical = 3.8).outlook())
    }

    @Test
    fun pastCriticalShowsFullOnly() {
        assertEquals(VolumeOutlook(full = 0), volume(full = 0.4, critical = null).outlook())
    }

    @Test
    fun beyondAYearReadsAsGrowing() {
        assertEquals(VolumeOutlook(growingPerDay = 0.1), volume(slope = 0.1, full = null, critical = null).outlook())
    }

    @Test
    fun lowConfidenceShowsTheGrowthOnlyWhenItGrows() {
        assertEquals(VolumeOutlook(growingPerDay = 0.4), volume(slope = 0.4, confidence = "low").outlook())
        assertNull(volume(slope = 0.0, full = null, critical = null, confidence = "low").outlook())
        assertNull(volume(slope = -1.0, full = null, critical = null, confidence = "low").outlook())
        assertNull(volume(slope = 0.01, full = null, critical = null, confidence = "low").outlook())
    }

    @Test
    fun theSlopeHasOneDecimalUnderTen() {
        assertEquals("2.4", formatSlope(2.4))
        assertEquals("0.1", formatSlope(0.06))
        assertEquals("12", formatSlope(12.3))
    }
}
