package name.levis.ichor.ui.nav

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class MaskResetTest {

    @Test
    fun privacyPageKeepsSettingsOverTheHome() {
        assertEquals(
            listOf(Routes.OVERVIEW, Routes.SETTINGS, "settings/privacy"),
            maskResetStack(Routes.OVERVIEW, Routes.SETTINGS_PAGE, "privacy"),
        )
    }

    @Test
    fun settingsKeepsItselfOverTheHome() {
        assertEquals(listOf(Routes.OVERVIEW, Routes.SETTINGS), maskResetStack(Routes.OVERVIEW, Routes.SETTINGS, null))
    }

    @Test
    fun aSwitchEndedWhileNotComposedIsAppliedOnTheNextComposition() {
        val mark = MaskResetMark(resetAt = 0)
        assertFalse(mark.due(0))
        // Locked or in the background: the switch started and ended unseen.
        assertTrue(mark.due(2))
        mark.done(2)
        assertFalse(mark.due(2))
    }

    @Test
    fun noResetWhileASwitchRuns() {
        val mark = MaskResetMark(resetAt = 2)
        assertFalse(mark.due(3))
        assertTrue(mark.due(4))
    }

    @Test
    fun anyOtherScreenGoesBackToTheStart() {
        assertEquals(listOf(Routes.OVERVIEW), maskResetStack(Routes.OVERVIEW, "node/{node}", null))
        assertEquals(listOf(Routes.IMPORT), maskResetStack(Routes.IMPORT, null, null))
    }
}
