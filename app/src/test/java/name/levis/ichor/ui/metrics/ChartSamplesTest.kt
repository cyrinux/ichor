package name.levis.ichor.ui.metrics

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class ChartSamplesTest {

    @Test
    fun theFirstStepStartsFromTheLatestValueShown() {
        assertEquals(3, stepSample(current = null, latest = 4, count = 6, delta = -1))
        assertEquals(5, stepSample(current = null, latest = 4, count = 6, delta = 1))
    }

    @Test
    fun laterStepsMoveFromTheSelectedSample() {
        assertEquals(1, stepSample(current = 2, latest = 4, count = 6, delta = -1))
        assertEquals(3, stepSample(current = 2, latest = 4, count = 6, delta = 1))
    }

    @Test
    fun stepsStopAtBothEnds() {
        assertEquals(0, stepSample(current = 0, latest = 4, count = 6, delta = -1))
        assertEquals(5, stepSample(current = 5, latest = 4, count = 6, delta = 1))
    }

    @Test
    fun withOnlyGapsStepsStartFromTheEnd() {
        assertEquals(4, stepSample(current = null, latest = null, count = 6, delta = -1))
    }

    @Test
    fun noTimesNoSample() {
        assertNull(stepSample(current = null, latest = null, count = 0, delta = 1))
    }

    @Test
    fun theAnnouncementHasTheTimeAndEachLineWithItsUnit() {
        val lines = listOf(TimeLine("cpu", listOf(1.0, 2.0)), TimeLine("memory", listOf(3.0, null)))
        val format = { v: Double -> "$v %" }
        assertEquals("10:00. cpu: 1.0 %, memory: 3.0 %", sampleAnnouncement("10:00", lines, 0, format, "no value"))
        assertEquals("10:05. cpu: 2.0 %, memory: no value", sampleAnnouncement("10:05", lines, 1, format, "no value"))
    }
}
