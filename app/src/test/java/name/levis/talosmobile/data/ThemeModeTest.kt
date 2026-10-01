package name.levis.talosmobile.data

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ThemeModeTest {

    @Test
    fun autoFollowsSystem() {
        assertTrue(ThemeMode.AUTO.isDark(systemDark = true))
        assertFalse(ThemeMode.AUTO.isDark(systemDark = false))
    }

    @Test
    fun explicitModesIgnoreSystem() {
        assertFalse(ThemeMode.LIGHT.isDark(systemDark = true))
        assertTrue(ThemeMode.DARK.isDark(systemDark = false))
        assertTrue(ThemeMode.BLACK.isDark(systemDark = false))
    }

    @Test
    fun parseFallsBackToAuto() {
        assertEquals(ThemeMode.BLACK, ThemeMode.parse("BLACK"))
        assertEquals(ThemeMode.AUTO, ThemeMode.parse(null))
        assertEquals(ThemeMode.AUTO, ThemeMode.parse("purple"))
    }
}
