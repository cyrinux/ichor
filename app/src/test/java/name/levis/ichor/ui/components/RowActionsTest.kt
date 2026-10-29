package name.levis.ichor.ui.components

import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.unit.dp
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class RowActionsTest {
    private val icon = ImageVector.Builder("test", 24.dp, 24.dp, 24f, 24f).build()

    private fun action(label: String, enabled: Boolean = true, destructive: Boolean = false) =
        RowAction(icon, label, onClick = {}, enabled = enabled, destructive = destructive)

    @Test
    fun bothEdgesDragWhenTheirActionsAreEnabled() {
        assertEquals(SwipeEdges(fromStart = true, fromEnd = true), swipeEdges(action("Sync"), action("Refresh"), swipeEnabled = true))
    }

    @Test
    fun aMissingOrDisabledActionLocksItsEdge() {
        assertEquals(SwipeEdges(fromStart = false, fromEnd = true), swipeEdges(null, action("Actions"), swipeEnabled = true))
        assertEquals(SwipeEdges(fromStart = false, fromEnd = true), swipeEdges(action("Live", enabled = false), action("Actions"), swipeEnabled = true))
        assertEquals(SwipeEdges(fromStart = true, fromEnd = false), swipeEdges(action("Sync"), null, swipeEnabled = true))
    }

    @Test
    fun swipingOffLocksBothEdges() {
        assertEquals(SwipeEdges(fromStart = false, fromEnd = false), swipeEdges(action("Sync"), action("Refresh"), swipeEnabled = false))
    }

    @Test
    fun accessibilityListsEachLabelOnce() {
        val sync = action("Sync")
        val refresh = action("Refresh")
        val labels = accessibleActions(sync, refresh, listOf(sync, refresh, action("Select"))).map { it.label }
        assertEquals(listOf("Sync", "Refresh", "Select"), labels)
    }

    @Test
    fun accessibilityLeavesOutDisabledActions() {
        val labels = accessibleActions(action("Live", enabled = false), action("Actions"), listOf(action("Delete", enabled = false))).map { it.label }
        assertEquals(listOf("Actions"), labels)
    }

    @Test
    fun accessibilityDropsTheEdgesWhileSwipingIsOff() {
        val labels = accessibleActions(action("Sync"), action("Refresh"), listOf(action("Select")), swipeEnabled = false).map { it.label }
        assertEquals(listOf("Select"), labels)
    }

    @Test
    fun noActionsGiveAnEmptyList() {
        assertTrue(accessibleActions(null, null, emptyList()).isEmpty())
    }

    @Test
    fun theToneFollowsTheEdgeUnlessDestructive() {
        assertEquals(SwipeTone.PRIMARY, swipeTone(action("Sync"), leading = true))
        assertEquals(SwipeTone.TERTIARY, swipeTone(action("Refresh"), leading = false))
        assertEquals(SwipeTone.ERROR, swipeTone(action("Delete", destructive = true), leading = false))
        assertEquals(SwipeTone.ERROR, swipeTone(action("Delete", destructive = true), leading = true))
    }
}
