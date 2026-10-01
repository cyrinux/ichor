package name.levis.talosmobile.model

import org.junit.Assert.assertEquals
import org.junit.Test

class NodeTimeTest {

    private fun t(offset: Long, error: String? = null) = NodeTime(node = "n", offsetMs = offset, error = error)

    @Test
    fun driftThresholds() {
        assertEquals(DriftLevel.OK, t(0).drift)
        assertEquals(DriftLevel.OK, t(-499).drift)
        assertEquals(DriftLevel.WARN, t(500).drift)
        assertEquals(DriftLevel.WARN, t(-4_999).drift)
        assertEquals(DriftLevel.BAD, t(5_000).drift)
        assertEquals(DriftLevel.BAD, t(-60_000).drift)
        assertEquals(DriftLevel.BAD, t(0, error = "unreachable").drift)
    }

    @Test
    fun worstOfCluster() {
        assertEquals(DriftLevel.OK, worstDrift(emptyList()))
        assertEquals(DriftLevel.WARN, worstDrift(listOf(t(1), t(700))))
        assertEquals(DriftLevel.BAD, worstDrift(listOf(t(1), t(700), t(0, "x"))))
    }

    @Test
    fun offsetFormatting() {
        assertEquals("+0 ms", formatOffset(0))
        assertEquals("+12 ms", formatOffset(12))
        assertEquals("−999 ms", formatOffset(-999))
        assertEquals("+1.25 s", formatOffset(1_250))
        assertEquals("−3 min 4 s", formatOffset(-184_000))
    }
}
