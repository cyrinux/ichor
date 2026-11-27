package name.levis.ichor.ui.components

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class PanesTest {
    @Test
    fun aPhoneStaysSinglePane() {
        assertFalse(twoPanes(360))
    }

    @Test
    fun aSevenInchPortraitOrAFoldedFoldableStaysSinglePane() {
        assertFalse(twoPanes(600))
    }

    @Test
    fun justUnderTheExpandedWidthStaysSinglePane() {
        assertFalse(twoPanes(839))
    }

    @Test
    fun theExpandedWidthShowsTwoPanes() {
        assertTrue(twoPanes(840))
    }

    @Test
    fun aTabletInLandscapeShowsTwoPanes() {
        assertTrue(twoPanes(1280))
    }

    private data class Row(val id: String, val value: Int)

    @Test
    fun nothingPickedShowsThePlaceholder() {
        assertNull(pickedById(listOf(Row("a", 1)), null) { it.id })
    }

    @Test
    fun aListNotReadYetShowsThePlaceholder() {
        assertNull(pickedById<Row>(null, "a") { it.id })
    }

    @Test
    fun thePickIsTheLiveRowWithItsId() {
        val rows = listOf(Row("a", 1), Row("b", 2))
        assertEquals(Row("b", 2), pickedById(rows, "b") { it.id })
    }

    @Test
    fun aPickGoneFromTheListShowsThePlaceholder() {
        assertNull(pickedById(listOf(Row("a", 1)), "b") { it.id })
    }
}
