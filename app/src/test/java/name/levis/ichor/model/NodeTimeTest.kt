package name.levis.ichor.model

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
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
        assertNull(t(0, error = "unreachable").drift)
    }

    @Test
    fun summaryAllInSync() {
        val s = driftSummary(listOf(t(3), t(-9), t(1)))
        assertEquals(DriftLevel.OK, s.level)
        assertEquals(3, s.reachable)
        assertEquals(0, s.unreachable)
        assertEquals(0, s.drifting)
        assertEquals(9L, s.maxOffsetMs)
        assertFalse(s.expandedByDefault)
    }

    @Test
    fun unreachableNodesDoNotMakeItOutOfSync() {
        val s = driftSummary(listOf(t(3), t(-9), t(0, error = "rpc error: dial tcp 10.0.0.1:50000")))
        assertEquals(DriftLevel.OK, s.level)
        assertEquals(2, s.reachable)
        assertEquals(1, s.unreachable)
        assertEquals(9L, s.maxOffsetMs)
        assertFalse(s.expandedByDefault)
    }

    @Test
    fun driftingExpandsByDefault() {
        val warn = driftSummary(listOf(t(3), t(-700), t(0, "x")))
        assertEquals(DriftLevel.WARN, warn.level)
        assertEquals(1, warn.drifting)
        assertEquals(700L, warn.maxOffsetMs)
        assertTrue(warn.expandedByDefault)

        val bad = driftSummary(listOf(t(600), t(-6_000)))
        assertEquals(DriftLevel.BAD, bad.level)
        assertEquals(2, bad.drifting)
        assertTrue(bad.expandedByDefault)
    }

    @Test
    fun noReachableNode() {
        val s = driftSummary(listOf(t(0, "x"), t(0, "y")))
        assertNull(s.level)
        assertNull(s.maxOffsetMs)
        assertEquals(2, s.unreachable)
        assertFalse(s.expandedByDefault)
        assertNull(driftSummary(emptyList()).level)
    }

    @Test
    fun offsetFormatting() {
        assertEquals("+0 ms", formatOffset(0))
        assertEquals("+12 ms", formatOffset(12))
        assertEquals("−999 ms", formatOffset(-999))
        assertEquals("+1.25 s", formatOffset(1_250))
        assertEquals("−3 min 4 s", formatOffset(-184_000))
        assertEquals("±9 ms", formatMaxOffset(9))
        assertEquals("±1.25 s", formatMaxOffset(-1_250))
    }
}
