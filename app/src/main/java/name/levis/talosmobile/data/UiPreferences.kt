package name.levis.talosmobile.data

import android.content.SharedPreferences
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

enum class ThemeMode(val label: String) {
    AUTO("Auto"),
    LIGHT("Light"),
    DARK("Dark"),
    BLACK("Black"),
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

class UiPreferences(private val prefs: SharedPreferences) {
    private val _themeMode = MutableStateFlow(ThemeMode.parse(prefs.getString(KEY_THEME, null)))
    val themeMode: StateFlow<ThemeMode> = _themeMode.asStateFlow()

    /** When the app lock is on, screenshots and the recents preview are blocked unless this is set. */
    private val _allowScreenshots = MutableStateFlow(prefs.getBoolean(KEY_SCREENSHOTS, false))
    val allowScreenshots: StateFlow<Boolean> = _allowScreenshots.asStateFlow()

    fun setAllowScreenshots(allow: Boolean) {
        prefs.edit().putBoolean(KEY_SCREENSHOTS, allow).apply()
        _allowScreenshots.value = allow
    }

    fun setThemeMode(mode: ThemeMode) {
        prefs.edit().putString(KEY_THEME, mode.name).apply()
        _themeMode.value = mode
    }

    private companion object {
        const val KEY_THEME = "theme_mode"
        const val KEY_SCREENSHOTS = "allow_screenshots"
    }
}
