package name.levis.ichor.ui.insights

import name.levis.ichor.model.DriftChange
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class BaselineStateTest {
    private val change = DriftChange(node = "192.0.2.1", reference = "192.0.2.2", key = "kernel", before = "a", after = "b")
    private val writing = InsightsState(changes = listOf(change), baselineAt = 100L, error = "old", baselineBusy = true)

    @Test
    fun failedKeepsTheBaselineAndTheDifferences() {
        val failed = writing.baselineFailed("disk full")
        assertEquals(100L, failed.baselineAt)
        assertEquals(listOf(change), failed.changes)
        assertEquals("disk full", failed.error)
        assertFalse(failed.baselineBusy)
    }

    @Test
    fun savedShowsTheNewTimeAndTheGivenDifferences() {
        val saved = writing.baselineSaved(200L, emptyList())
        assertEquals(200L, saved.baselineAt)
        assertTrue(saved.changes.isEmpty())
        assertNull(saved.error)
        assertFalse(saved.baselineBusy)
    }

    @Test
    fun deletedClearsTheTime() {
        val deleted = writing.baselineDeleted(listOf(change))
        assertNull(deleted.baselineAt)
        assertEquals(listOf(change), deleted.changes)
        assertNull(deleted.error)
        assertFalse(deleted.baselineBusy)
    }
}
