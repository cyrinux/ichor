package name.levis.ichor.security

import android.content.SharedPreferences
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.isDemo

/** Persistence for the app-lock preference and the enrolled security keys (abstracted for unit tests). */
interface LockSettings {
    var lockEnabled: Boolean

    /** The enrolled security keys (public data only), null when none. */
    var securityKeys: SecurityKeyEnrolment?
}

class PrefsLockSettings(private val prefs: SharedPreferences) : LockSettings {
    override var lockEnabled: Boolean
        get() = prefs.getBoolean(KEY, false)
        set(value) {
            prefs.edit().putBoolean(KEY, value).apply()
        }

    override var securityKeys: SecurityKeyEnrolment?
        get() = SecurityKeyEnrolment.decode(prefs.getString(KEY_SECURITY_KEYS, null))
        set(value) {
            prefs.edit().apply {
                if (value == null) remove(KEY_SECURITY_KEYS) else putString(KEY_SECURITY_KEYS, value.encode())
            }.apply()
        }

    private companion object {
        const val KEY = "app_lock_enabled"
        const val KEY_SECURITY_KEYS = "security_keys"
    }
}

/**
 * The lock is mandatory once the client keys of a real cluster are stored (the demo has none):
 * the app asks to set it up before showing anything else, and Settings cannot turn it off.
 */
fun lockRequired(contexts: List<ContextSummary>): Boolean = contexts.any { !it.isDemo }

/**
 * App lock: locked at cold start and after [graceMillis] in the background.
 * The grace period keeps short trips (file picker, camera permission) from relocking.
 *
 * Security keys (SecurityKeys.kt) can open it too. With one required, this also holds the data
 * key a tap unwrapped, for the life of the process: the credential files are sealed with it
 * ([DekHolder], see SecureStore), so a cold start reads nothing until a key is tapped, while
 * the app, once open, keeps working through relocks (its state stays loaded anyway).
 */
class AppLock(
    private val settings: LockSettings,
    private val clock: () -> Long,
    private val graceMillis: Long = 30_000,
) : DekHolder {
    private val _enabled = MutableStateFlow(settings.lockEnabled)
    val enabled: StateFlow<Boolean> = _enabled.asStateFlow()

    private val _securityKeys = MutableStateFlow(settings.securityKeys)
    /** The enrolled security keys, null when none. */
    val securityKeys: StateFlow<SecurityKeyEnrolment?> = _securityKeys.asStateFlow()

    /** Whether a key must be tapped (fingerprint / PIN alone is refused and the files are sealed). */
    val requiresKey: Boolean get() = _securityKeys.value?.required == true

    private var dek: ByteArray? = null

    override fun dek(): ByteArray? = dek
    override fun sealing(): Boolean = requiresKey

    /** The data key a tapped key unwrapped (or a new one, when the requirement is turned on). */
    fun provideDek(value: ByteArray) {
        dek = value.copyOf()
    }

    /** Forgets the data key: after the requirement was turned off, or the keys were removed. */
    fun clearDek() {
        dek?.fill(0)
        dek = null
    }

    /** Callers must have authenticated the user before changing this. */
    fun setSecurityKeys(enrolment: SecurityKeyEnrolment?) {
        val value = enrolment?.takeIf { it.keys.isNotEmpty() }
        settings.securityKeys = value
        _securityKeys.value = value
        if (value?.required != true) clearDek()
    }

    private val _locked = MutableStateFlow(settings.lockEnabled)
    val locked: StateFlow<Boolean> = _locked.asStateFlow()

    /**
     * Whether the app was unlocked (or needed no unlock) since the process started. Held here,
     * not in saved instance state: that is restored after a process death, when [locked] is
     * true again and nothing may load before the user authenticates.
     */
    private val _everUnlocked = MutableStateFlow(!settings.lockEnabled)
    val everUnlocked: StateFlow<Boolean> = _everUnlocked.asStateFlow()

    private var backgroundedAt: Long? = null

    /** Callers must have authenticated the user before changing this; the lock off drops the enrolled keys too. */
    fun setEnabled(enabled: Boolean) {
        settings.lockEnabled = enabled
        _enabled.value = enabled
        if (!enabled) {
            setSecurityKeys(null)
            unlock()
        }
    }

    fun unlock() {
        _everUnlocked.value = true
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
