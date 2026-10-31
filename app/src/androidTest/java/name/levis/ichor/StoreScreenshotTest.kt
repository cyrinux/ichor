package name.levis.ichor

import android.os.Build
import androidx.compose.ui.test.junit4.createEmptyComposeRule
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import name.levis.ichor.i18n.AppLocale
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assume.assumeTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import java.util.Locale

/**
 * The Play listing's screens in every language, from the offline demo (scripts/store-screenshots.sh,
 * the store job of workflow screenshots.yml). Ten walks take half an hour: it runs only with the
 * instrumentation argument `screenshots=true`, and ui-smoke.sh leaves the class out too.
 */
@RunWith(AndroidJUnit4::class)
class StoreScreenshotTest {
    @get:Rule
    val compose = createEmptyComposeRule()

    private val app get() = ApplicationProvider.getApplicationContext<TalosApp>()

    @After
    fun backToTheSystemLanguage() {
        if (AppLocale.systemManaged) AppLocale.apply(app, AppLocale.SYSTEM)
    }

    @Test
    fun captureEveryLanguage() {
        assumeTrue(InstrumentationRegistry.getArguments().getString("screenshots") == "true")
        assumeTrue(Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU)
        AppLocale.languages.forEach { language ->
            val folder = requireNotNull(PLAY_FOLDERS[language.tag]) { "no Play folder for ${language.tag}" }
            AppLocale.apply(app, language.tag)
            val walk = DemoWalk(compose, "$DIR/$folder")
            assertShows(walk, language.tag)
            walk.captureListingScreens()
        }
    }

    /** A set in the wrong language must fail here, not reach the store. */
    private fun assertShows(walk: DemoWalk, tag: String) {
        walk.openDemo().use { scenario ->
            var shown = ""
            scenario.onActivity { shown = it.resources.configuration.locales[0].language }
            assertEquals(Locale.forLanguageTag(tag).language, shown)
        }
    }

    private companion object {
        /** Where the raw captures go: the shell user writes here, store-screenshots.sh pulls it. */
        const val DIR = "/data/local/tmp/ichor-store-screenshots"

        /** AppLocale's tags to the folders of fastlane/metadata/android/. */
        val PLAY_FOLDERS = mapOf(
            "en" to "en-US",
            "fr" to "fr-FR",
            "es" to "es-ES",
            "uk" to "uk",
            "de" to "de-DE",
            "it" to "it-IT",
            "zh-CN" to "zh-CN",
            "ru" to "ru-RU",
            "pt-BR" to "pt-BR",
            "ja" to "ja-JP",
        )
    }
}
