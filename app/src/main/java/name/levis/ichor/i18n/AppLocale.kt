package name.levis.ichor.i18n

import android.app.LocaleManager
import android.content.Context
import android.content.res.Configuration
import android.os.Build
import android.os.LocaleList
import name.levis.ichor.data.UiPreferences
import java.util.Locale

/** A language the UI is translated into; [nativeName] is always shown in that language. */
data class AppLanguage(val tag: String, val nativeName: String)

/** In-app language choice. An empty tag means "follow the system". */
object AppLocale {
    const val SYSTEM = ""

    /** The JVM default before any override, restored when going back to the system language. */
    private val systemDefault: Locale = Locale.getDefault()

    val languages = listOf(
        AppLanguage("en", "English"),
        AppLanguage("fr", "Français"),
        AppLanguage("es", "Español"),
        AppLanguage("uk", "Українська"),
        AppLanguage("de", "Deutsch"),
        AppLanguage("it", "Italiano"),
        // The region picks values-zh-rCN (Simplified Chinese).
        AppLanguage("zh-CN", "简体中文"),
        AppLanguage("pt-BR", "Português (Brasil)"),
        AppLanguage("ja", "日本語"),
    )

    /** Normalizes a stored or system tag to a supported one, or [SYSTEM]. */
    fun normalize(tag: String?): String {
        val language = tag?.substringBefore(',')?.let { Locale.forLanguageTag(it).language }.orEmpty()
        return languages.firstOrNull { Locale.forLanguageTag(it.tag).language == language }?.tag ?: SYSTEM
    }

    /** Whether Android manages per-app languages itself (API 33+). */
    val systemManaged: Boolean get() = Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU

    /**
     * Applies [tag] app-wide. On API 33+ the system stores it (and keeps its per-app language
     * setting in sync) and recreates activities; returns whether the caller must recreate.
     */
    fun apply(context: Context, tag: String): Boolean {
        if (systemManaged) {
            context.getSystemService(LocaleManager::class.java).applicationLocales =
                if (tag == SYSTEM) LocaleList.getEmptyLocaleList() else LocaleList.forLanguageTags(tag)
            return false
        }
        return true
    }

    /** The per-app language the system holds (API 33+), or null below. */
    fun systemChoice(context: Context): String? {
        if (!systemManaged) return null
        val locales = context.getSystemService(LocaleManager::class.java).applicationLocales
        return if (locales.isEmpty) SYSTEM else normalize(locales.toLanguageTags())
    }

    /**
     * The language the app shows ("fr"), for what Go translates outside any screen (the
     * background monitor's plugin findings): the user's choice, else the system's.
     */
    fun language(context: Context): String {
        val chosen = systemChoice(context) ?: UiPreferences.storedLanguage(context)
        return if (chosen == SYSTEM) Locale.getDefault().language else Locale.forLanguageTag(chosen).language
    }

    /**
     * Below API 33, a context whose resources use the stored language (activities in
     * attachBaseContext, notifications, the widget). On API 33+ the system already does it.
     */
    fun wrap(base: Context): Context {
        if (systemManaged) return base
        val tag = UiPreferences.storedLanguage(base)
        if (tag == SYSTEM) {
            Locale.setDefault(systemDefault)
            return base
        }
        val locale = Locale.forLanguageTag(tag)
        Locale.setDefault(locale)
        val config = Configuration(base.resources.configuration).apply {
            setLocales(LocaleList(locale))
        }
        return base.createConfigurationContext(config)
    }
}
