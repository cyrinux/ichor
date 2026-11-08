package name.levis.ichor.ui.components

import org.junit.Assert.assertEquals
import org.junit.Test

class CardListTest {
    @Test
    fun aSingleRowIsTheWholeCard() {
        assertEquals(RowPosition.ONLY, rowPosition(0, 1))
    }

    @Test
    fun twoRowsAreTheFirstAndTheLast() {
        assertEquals(RowPosition.FIRST, rowPosition(0, 2))
        assertEquals(RowPosition.LAST, rowPosition(1, 2))
    }

    @Test
    fun rowsBetweenAreInTheMiddle() {
        assertEquals(listOf(RowPosition.FIRST, RowPosition.MIDDLE, RowPosition.LAST), (0..2).map { rowPosition(it, 3) })
    }

    @Test(expected = IllegalArgumentException::class)
    fun aRowPastTheEndIsRefused() {
        rowPosition(3, 3)
    }

    @Test(expected = IllegalArgumentException::class)
    fun aNegativeRowIsRefused() {
        rowPosition(-1, 2)
    }

    @Test
    fun groupsComeInOrderOfFirstAppearanceAndKeepTheirItemOrder() {
        val rows = listOf("web/db" to 1, "shop/api" to 2, "web/api" to 3, "shop/cart" to 4)
        val groups = groupInOrder(rows) { it.first.substringBefore('/') }
        assertEquals(listOf("web", "shop"), groups.map { it.first })
        assertEquals(listOf(1, 3), groups[0].second.map { it.second })
        assertEquals(listOf(2, 4), groups[1].second.map { it.second })
    }

    @Test
    fun noItemsMakeNoGroups() {
        assertEquals(emptyList<Pair<String, List<String>>>(), groupInOrder(emptyList<String>()) { it })
    }
}
