package name.levis.talosmobile.data

import name.levis.talosmobile.model.ConfigSummary
import name.levis.talosmobile.model.ContextSummary
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class ActiveContextTest {

    private fun summary(vararg names: String, current: String = names.first()) =
        ConfigSummary(current = current, contexts = names.map { ContextSummary(name = it) })

    private val real = summary("admin@prod", "admin@staging", current = "admin@prod")
    private val masked = summary("admin@homelab", "admin@homelab-2", current = "admin@homelab")

    @Test
    fun savedPositionSurvivesMasking() {
        // Selected while masked: the saved name is a fake, the position still points right.
        assertEquals("admin@staging", resolveActive(real, "admin@homelab-2", 1))
        assertEquals("admin@homelab-2", resolveActive(masked, "admin@staging", 1))
    }

    @Test
    fun nameWhenNoPositionWasSaved() {
        assertEquals("admin@staging", resolveActive(real, "admin@staging", -1))
    }

    @Test
    fun fallsBackToCurrent() {
        assertEquals("admin@prod", resolveActive(real, "gone", -1))
        assertEquals("admin@prod", resolveActive(real, null, 7))
    }

    @Test
    fun contextAtMapsAcrossMaskChange() {
        assertEquals("admin@homelab-2", contextAt(masked, 1))
        assertEquals("admin@homelab", contextAt(masked, -1))
    }

    @Test
    fun adjacentContextStopsAtBothEnds() {
        val three = summary("a", "b", "c")
        assertEquals("b", adjacentContext(three, "a", 1))
        assertEquals("b", adjacentContext(three, "c", -1))
        assertNull(adjacentContext(three, "c", 1))
        assertNull(adjacentContext(three, "a", -1))
        assertNull(adjacentContext(three, "b", 0))
        assertNull(adjacentContext(three, "gone", 1))
    }

    @Test
    fun activeFollowsItsContextWhenAnotherIsRemoved() {
        // Positions among a, b, c, d (4 contexts, 3 remaining).
        assertEquals(0, activeIndexAfterRemoval(active = 0, removed = 2, remaining = 3))
        assertEquals(1, activeIndexAfterRemoval(active = 2, removed = 0, remaining = 3))
    }

    @Test
    fun removingTheActiveContextShowsItsNeighbour() {
        assertEquals(1, activeIndexAfterRemoval(active = 1, removed = 1, remaining = 3))
        // The last one: the one before it.
        assertEquals(2, activeIndexAfterRemoval(active = 3, removed = 3, remaining = 3))
        assertEquals(0, activeIndexAfterRemoval(active = 0, removed = 0, remaining = 1))
    }
}
