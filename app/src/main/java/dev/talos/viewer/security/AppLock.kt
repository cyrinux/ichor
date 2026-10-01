package dev.talos.viewer.security

import android.content.SharedPreferences
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/** Persistence for the app-lock preference (abstracted for unit tests). */
interface LockSettings {
    var lockEnabled: Boolean
}

class PrefsLockSettings(private val prefs: SharedPreferences) : LockSettings {
    override var lockEnabled: Boolean
        get() = prefs.getBoolean(KEY, false)
        set(value) {
            prefs.edit().putBoolean(KEY, value).apply()
        }

    private companion object {
        const val KEY = "app_lock_enabled"
    }
}

/**
 * Optional app lock: locked at cold start and after [graceMillis] in the background.
 * The grace period keeps short trips (file picker, camera permission) from relocking.
 */
class AppLock(
    private val settings: LockSettings,
    private val clock: () -> Long,
    private val graceMillis: Long = 30_000,
) {
    private val _enabled = MutableStateFlow(settings.lockEnabled)
    val enabled: StateFlow<Boolean> = _enabled.asStateFlow()

    private val _locked = MutableStateFlow(settings.lockEnabled)
    val locked: StateFlow<Boolean> = _locked.asStateFlow()

    private var backgroundedAt: Long? = null

    /** Callers must have authenticated the user before changing this. */
    fun setEnabled(enabled: Boolean) {
        settings.lockEnabled = enabled
        _enabled.value = enabled
        if (!enabled) _locked.value = false
    }

    fun unlock() {
        _locked.value = false
    }

    fun onBackground() {
        backgroundedAt = clock()
    }

    fun onForeground() {
        val since = backgroundedAt ?: return
        backgroundedAt = null
        if (_enabled.value && clock() - since >= graceMillis) _locked.value = true
    }
}
