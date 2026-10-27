package name.levis.ichor.i18n

import org.junit.Assert.assertEquals
import org.junit.Test

class AppLocaleTest {
    @Test
    fun aTagMatchesItsLanguageWhateverTheRegion() {
        assertEquals("fr", AppLocale.normalize("fr-CA"))
        assertEquals("zh-CN", AppLocale.normalize("zh-CN"))
        assertEquals("zh-CN", AppLocale.normalize("zh-Hans-CN"))
        assertEquals("uk", AppLocale.normalize("uk,en"))
    }

    @Test
    fun anUnknownOrEmptyTagFollowsTheSystem() {
        assertEquals(AppLocale.SYSTEM, AppLocale.normalize("ko"))
        assertEquals(AppLocale.SYSTEM, AppLocale.normalize(""))
        assertEquals(AppLocale.SYSTEM, AppLocale.normalize(null))
    }
}
