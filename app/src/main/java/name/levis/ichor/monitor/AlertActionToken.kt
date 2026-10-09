package name.levis.ichor.monitor

import android.content.SharedPreferences
import java.security.MessageDigest
import java.security.SecureRandom
import java.util.Base64

/**
 * A random secret of this install, made once, that every in-app alert button carries: the main
 * activity is exported (share links), so another app could send it the same extras. Only an
 * intent with the token asks for a confirmation; without it, the link opens its screen only.
 */
class AlertActionToken(private val prefs: SharedPreferences) {

    /** The token, made on first use. */
    @Synchronized
    fun value(): String = prefs.getString(KEY, null) ?: ByteArray(BYTES).also(SecureRandom()::nextBytes)
        .let { Base64.getEncoder().encodeToString(it) }
        .also { prefs.edit().putString(KEY, it).commit() }

    /** Whether [candidate] is the token, compared in constant time; false before any was made. */
    fun accepts(candidate: String?): Boolean {
        val token = prefs.getString(KEY, null) ?: return false
        if (candidate.isNullOrEmpty()) return false
        return MessageDigest.isEqual(token.toByteArray(), candidate.toByteArray())
    }

    private companion object {
        const val KEY = "action_token"
        const val BYTES = 32
    }
}
