package name.levis.ichor.ui.tour

import androidx.compose.ui.geometry.Rect
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class TourStepsTest {
    private val all = TourAnchor.entries.toSet()

    @Test
    fun theFiveStepsGoFromTheSwitcherToSettings() {
        assertEquals(
            listOf(TourAnchor.Switcher, TourAnchor.Nodes, TourAnchor.DegradedNode, TourAnchor.Actions, TourAnchor.Settings),
            demoTourSteps.map { it.anchor },
        )
    }

    @Test
    fun everyAnchorOnScreenIsShownInOrder() {
        val shown = generateSequence(tourPosition(demoTourSteps, 0, all)) { tourPosition(demoTourSteps, it.step + 1, all) }.toList()
        assertEquals((0..4).toList(), shown.map { it.step })
        assertEquals((1..5).toList(), shown.map { it.number })
        assertTrue(shown.all { it.total == 5 })
        assertTrue(shown.last().last)
    }

    @Test
    fun aStepWhoseAnchorIsNotOnScreenIsSkipped() {
        val present = all - TourAnchor.DegradedNode
        val second = tourPosition(demoTourSteps, 1, present)!!
        val next = tourPosition(demoTourSteps, second.step + 1, present)!!
        assertEquals(3, next.step)
        assertEquals(3, next.number)
        assertEquals(4, next.total)
    }

    @Test
    fun aMissingCurrentAnchorMovesOnToTheNextOne() {
        assertEquals(1, tourPosition(demoTourSteps, 0, all - TourAnchor.Switcher)!!.step)
    }

    @Test
    fun theTourEndsAfterTheLastStepOnScreen() {
        assertNull(tourPosition(demoTourSteps, 5, all))
        assertNull(tourPosition(demoTourSteps, 4, all - TourAnchor.Settings))
        assertNull(tourPosition(demoTourSteps, 0, emptySet()))
    }

    @Test
    fun anAnchorScrolledAwayOrNotLaidOutIsNotPresent() {
        val viewport = Rect(0f, 0f, 400f, 800f)
        val bounds = mapOf(
            TourAnchor.Switcher to Rect(10f, 10f, 200f, 60f),
            TourAnchor.Nodes to Rect(0f, 900f, 400f, 1200f),
            TourAnchor.DegradedNode to Rect.Zero,
        )
        assertEquals(setOf(TourAnchor.Switcher), presentAnchors(bounds, viewport))
    }
}
