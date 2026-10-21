package name.levis.ichor.ui.components

import android.content.pm.ActivityInfo.SCREEN_ORIENTATION_FULL_USER
import android.content.pm.ActivityInfo.SCREEN_ORIENTATION_UNSPECIFIED
import android.content.pm.ActivityInfo.SCREEN_ORIENTATION_USER_PORTRAIT
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class OrientationTest {
    @Test
    fun aSmallPhoneLocksPortrait() {
        assertTrue(locksPortrait(320))
    }

    @Test
    fun aPhoneLocksPortrait() {
        assertTrue(locksPortrait(360))
    }

    @Test
    fun justUnderATabletLocksPortrait() {
        assertTrue(locksPortrait(599))
    }

    @Test
    fun aSevenInchTabletOrAnUnfoldedFoldableRotates() {
        assertFalse(locksPortrait(600))
    }

    @Test
    fun aLargeTabletRotates() {
        assertFalse(locksPortrait(840))
    }

    @Test
    fun aPhoneStartsInPortrait() {
        assertEquals(SCREEN_ORIENTATION_USER_PORTRAIT, startOrientation(360, SCREEN_ORIENTATION_UNSPECIFIED, restoring = false))
    }

    @Test
    fun aPhoneRotatedOnAWideScreenKeepsItsLandscape() {
        assertEquals(SCREEN_ORIENTATION_FULL_USER, startOrientation(360, SCREEN_ORIENTATION_FULL_USER, restoring = true))
    }

    @Test
    fun aFreshStartIgnoresAnUnlockLeftOver() {
        assertEquals(SCREEN_ORIENTATION_USER_PORTRAIT, startOrientation(360, SCREEN_ORIENTATION_FULL_USER, restoring = false))
    }

    @Test
    fun aFoldedFoldableLocksAgainAfterUnfolded() {
        assertEquals(SCREEN_ORIENTATION_USER_PORTRAIT, startOrientation(360, SCREEN_ORIENTATION_UNSPECIFIED, restoring = true))
    }

    @Test
    fun aChartKeepsItsHeightInPortrait() {
        assertEquals(140, chartHeightDp(140, widthDp = 360, heightDp = 780))
    }

    @Test
    fun aChartGrowsInLandscape() {
        assertEquals(162, chartHeightDp(140, widthDp = 780, heightDp = 360))
    }

    @Test
    fun aChartNeverShrinksUnderItsMinimum() {
        assertEquals(140, chartHeightDp(140, widthDp = 640, heightDp = 280))
    }

    @Test
    fun aTabletAsksForNothing() {
        assertEquals(SCREEN_ORIENTATION_UNSPECIFIED, startOrientation(600, SCREEN_ORIENTATION_USER_PORTRAIT, restoring = true))
    }
}
