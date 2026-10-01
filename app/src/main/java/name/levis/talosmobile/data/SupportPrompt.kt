package name.levis.talosmobile.data

import android.content.SharedPreferences
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

const val REPO_URL_BASE = "https://github.com/"
const val TALOS_URL = "https://www.talos.dev"
const val SPONSOR_URL = "https://github.com/sponsors/cyrinux"
const val BTC_ADDRESS = "bc1qc0dhqrgw6z08du94rkfequk8n5r3lgcr5lnxtl"
const val ETH_ADDRESS = "0xb32676301F9c4abD35Eb2e4c7C8cdA754BA29804"

data class SupportState(val firstSeen: Long, val launches: Int, val lastAsked: Long, val never: Boolean) {
    /** Only after real use (2 weeks, 10 launches), at most every 90 days, never if declined for good. */
    fun shouldAsk(now: Long): Boolean =
        !never && launches >= MIN_LAUNCHES && now - firstSeen >= MIN_AGE && now - lastAsked >= INTERVAL

    private companion object {
        const val DAY = 86_400_000L
        const val MIN_LAUNCHES = 10
        const val MIN_AGE = 14 * DAY
        const val INTERVAL = 90 * DAY
    }
}

/** Occasional, dismissable "support the project" card on the overview. */
class SupportPrompt(private val prefs: SharedPreferences, private val clock: () -> Long = System::currentTimeMillis) {
    private val _visible = MutableStateFlow(false)
    val visible: StateFlow<Boolean> = _visible.asStateFlow()

    /** Call once per app launch. */
    fun onLaunch() {
        val now = clock()
        val firstSeen = prefs.getLong(KEY_FIRST, 0).takeIf { it > 0 } ?: now.also { prefs.edit().putLong(KEY_FIRST, it).apply() }
        val launches = prefs.getInt(KEY_LAUNCHES, 0) + 1
        prefs.edit().putInt(KEY_LAUNCHES, launches).apply()
        _visible.value = SupportState(firstSeen, launches, prefs.getLong(KEY_ASKED, 0), prefs.getBoolean(KEY_NEVER, false))
            .shouldAsk(now)
    }

    fun later() {
        prefs.edit().putLong(KEY_ASKED, clock()).apply()
        _visible.value = false
    }

    fun never() {
        prefs.edit().putBoolean(KEY_NEVER, true).apply()
        _visible.value = false
    }

    private companion object {
        const val KEY_FIRST = "first_seen"
        const val KEY_LAUNCHES = "launches"
        const val KEY_ASKED = "last_asked"
        const val KEY_NEVER = "never"
    }
}
