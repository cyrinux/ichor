package name.levis.ichor.ui.components

import org.junit.Assert.assertFalse
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
}
