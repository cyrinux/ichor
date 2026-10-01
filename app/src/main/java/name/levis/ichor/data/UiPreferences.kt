package name.levis.ichor.data

import android.content.Context
import android.content.SharedPreferences
import androidx.annotation.StringRes
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import name.levis.ichor.R

enum class ThemeMode(@StringRes val label: Int) {
    AUTO(R.string.settings_theme_auto),
    LIGHT(R.string.settings_theme_light),
    DARK(R.string.settings_theme_dark),
    BLACK(R.string.settings_theme_black),
    ;

    /** Whether this mode renders dark, given the system setting (used by AUTO). */
    fun isDark(systemDark: Boolean): Boolean = when (this) {
        AUTO -> systemDark
        LIGHT -> false
        DARK, BLACK -> true
    }

    companion object {
        fun parse(name: String?): ThemeMode = entries.firstOrNull { it.name == name } ?: AUTO
    }
}

/**
 * Screenshot mode: the Go core replaces IPs, hostnames, context names and [extraWords] in
 * everything it returns, so screens can be shared without leaking the cluster's identity.
 */
data class PrivacyMask(val enabled: Boolean = false, val extraWords: String = "") {
    /** [extraWords] as the Go core takes them: trimmed, comma-separated, no empty entries. */
    val words: String
        get() = extraWords.split(',').map { it.trim() }.filter { it.isNotEmpty() }.distinct().joinToString(",")
}

class UiPreferences(private val prefs: SharedPreferences) {
    private val _themeMode = MutableStateFlow(ThemeMode.parse(prefs.getString(KEY_THEME, null)))
    val themeMode: StateFlow<ThemeMode> = _themeMode.asStateFlow()

    /** When the app lock is on, screenshots and the recents preview are blocked unless this is set. */
    private val _allowScreenshots = MutableStateFlow(prefs.getBoolean(KEY_SCREENSHOTS, false))
    val allowScreenshots: StateFlow<Boolean> = _allowScreenshots.asStateFlow()

    /** In-app language as a BCP-47 tag; "" follows the system. */
    private val _language = MutableStateFlow(prefs.getString(KEY_LANGUAGE, "").orEmpty())
    val language: StateFlow<String> = _language.asStateFlow()

    private val _privacyMask = MutableStateFlow(
        PrivacyMask(prefs.getBoolean(KEY_PRIVACY_MASK, false), prefs.getString(KEY_PRIVACY_WORDS, "").orEmpty()),
    )
    val privacyMask: StateFlow<PrivacyMask> = _privacyMask.asStateFlow()

    /** Committed synchronously: the worker and widget read it back at process start. */
    fun setPrivacyMask(mask: PrivacyMask) {
        prefs.edit().putBoolean(KEY_PRIVACY_MASK, mask.enabled).putString(KEY_PRIVACY_WORDS, mask.extraWords).commit()
        _privacyMask.value = mask
    }

    fun setAllowScreenshots(allow: Boolean) {
        prefs.edit().putBoolean(KEY_SCREENSHOTS, allow).apply()
        _allowScreenshots.value = allow
    }

    fun setThemeMode(mode: ThemeMode) {
        prefs.edit().putString(KEY_THEME, mode.name).apply()
        _themeMode.value = mode
    }

    /** Committed synchronously: the activity is recreated right after and reads it back. */
    fun setLanguage(tag: String) {
        prefs.edit().putString(KEY_LANGUAGE, tag).commit()
        _language.value = tag
    }

    companion object {
        const val FILE = "talosdev-mobile-ui"
        private const val KEY_THEME = "theme_mode"
        private const val KEY_SCREENSHOTS = "allow_screenshots"
        private const val KEY_LANGUAGE = "language"
        private const val KEY_PRIVACY_MASK = "privacy_mask"
        private const val KEY_PRIVACY_WORDS = "privacy_mask_words"

        /** Reads the language without the app singletons (usable from attachBaseContext). */
        fun storedLanguage(context: Context): String =
            context.getSharedPreferences(FILE, Context.MODE_PRIVATE).getString(KEY_LANGUAGE, "").orEmpty()
    }
}
