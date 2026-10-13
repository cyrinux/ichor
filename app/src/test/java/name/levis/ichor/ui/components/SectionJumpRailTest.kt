package name.levis.ichor.ui.components

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class SectionJumpRailTest {
    private val summary = JumpSection("header", "Summary", 0)
    private val nodes = JumpSection("nodes-title", "Nodes", 1)
    private val workloads = JumpSection("workloads-title", "Workloads", 12)
    private val sections = listOf(summary, nodes, workloads)

    @Test
    fun currentSectionBeforeTheFirstHeaderIsTheFirst() {
        val late = listOf(JumpSection("a", "A", 3), JumpSection("b", "B", 8))
        assertEquals(late.first(), currentSection(late, firstVisibleIndex = 1, atEnd = false))
    }

    @Test
    fun currentSectionBetweenHeadersIsTheEarlierOne() {
        assertEquals(nodes, currentSection(sections, firstVisibleIndex = 5, atEnd = false))
        assertEquals(summary, currentSection(sections, firstVisibleIndex = 0, atEnd = false))
        assertEquals(workloads, currentSection(sections, firstVisibleIndex = 12, atEnd = false))
    }

    @Test
    fun currentSectionAtTheEndIsTheLast() {
        assertEquals(workloads, currentSection(sections, firstVisibleIndex = 4, atEnd = true))
    }

    @Test
    fun currentSectionOfNoSectionsIsNull() {
        assertNull(currentSection(emptyList(), firstVisibleIndex = 0, atEnd = false))
    }

    @Test
    fun sectionAtMapsTheEndsAndClamps() {
        assertEquals(summary, sectionAt(sections, 0f))
        assertEquals(workloads, sectionAt(sections, 1f))
        assertEquals(summary, sectionAt(sections, -0.4f))
        assertEquals(workloads, sectionAt(sections, 3f))
        assertEquals(nodes, sectionAt(sections, 0.5f))
    }

    @Test
    fun sectionAtSplitsTwoSectionsInHalf() {
        val two = listOf(summary, nodes)
        assertEquals(summary, sectionAt(two, 0.49f))
        assertEquals(nodes, sectionAt(two, 0.5f))
    }

    @Test
    fun sectionAtOfNoSectionsIsNull() {
        assertNull(sectionAt(emptyList(), 0.5f))
    }
}
