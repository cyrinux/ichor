package name.levis.ichor.ui.overview

import name.levis.ichor.model.OverviewCard
import org.junit.Assert.assertEquals
import org.junit.Test

class OverviewItemsTest {
    @Test
    fun onlyTheSummarySpansBothColumns() {
        assertEquals(listOf(OverviewCard.SUMMARY), OverviewCard.entries.filter(::spansBothColumns))
    }
}
