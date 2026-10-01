package name.levis.ichor.ui

import name.levis.ichor.model.NodeStats
import name.levis.ichor.model.ratesBetween
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class StatsTest {

    private fun sample(at: Long, busy: Double, total: Double, rx: Long, read: Long) = NodeStats(
        at = at, cpuBusy = busy, cpuTotal = total, cpuCount = 8,
        memTotal = 1000, memAvailable = 250, load1 = 1.5,
        netRx = rx, netTx = rx / 2, diskRead = read, diskWrite = read * 2,
    )

    @Test
    fun ratesOverTwoSeconds() {
        val p = ratesBetween(sample(0, 10.0, 100.0, 1_000, 0), sample(2_000, 30.0, 180.0, 5_000, 4_096))!!
        assertEquals(25f, p.cpuPercent, 0.001f) // 20 busy of 80 total
        assertEquals(75f, p.memPercent, 0.001f)
        assertEquals(750L, p.memUsed)
        assertEquals(2_000f, p.rxPerSec, 0.001f)
        assertEquals(1_000f, p.txPerSec, 0.001f)
        assertEquals(2_048f, p.readPerSec, 0.001f)
        assertEquals(4_096f, p.writePerSec, 0.001f)
    }

    @Test
    fun counterResetGivesZeroNotNegative() {
        val p = ratesBetween(sample(0, 500.0, 900.0, 9_000, 9_000), sample(2_000, 5.0, 10.0, 100, 100))!!
        assertEquals(0f, p.rxPerSec)
        assertEquals(0f, p.readPerSec)
        assertEquals(0f, p.cpuPercent)
    }

    @Test
    fun noElapsedTimeIsSkipped() {
        assertNull(ratesBetween(sample(5, 1.0, 2.0, 1, 1), sample(5, 1.0, 2.0, 1, 1)))
    }
}
