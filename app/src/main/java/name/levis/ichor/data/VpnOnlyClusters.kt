package name.levis.ichor.data

import android.content.SharedPreferences
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import name.levis.ichor.model.ConfigSummary
import name.levis.ichor.model.keepVpnOnly

/**
 * A set of clusters by context fingerprint, kept in its own [prefs] file. Committed
 * synchronously: the monitor worker reads it back at process start. Only on this device.
 */
open class FingerprintSet(private val prefs: SharedPreferences) {
    private val _fingerprints = MutableStateFlow(prefs.getStringSet(KEY, null)?.toSet().orEmpty())
    val fingerprints: StateFlow<Set<String>> = _fingerprints.asStateFlow()

    fun set(fingerprint: String, member: Boolean) {
        if (fingerprint.isBlank()) return
        store(if (member) _fingerprints.value + fingerprint else _fingerprints.value - fingerprint)
    }

    /** Forgets the clusters no longer in [summary]. */
    fun sync(summary: ConfigSummary) = store(keepVpnOnly(_fingerprints.value, summary.contexts.map { it.fingerprint }))

    private fun store(fingerprints: Set<String>) {
        if (fingerprints == _fingerprints.value) return
        prefs.edit().putStringSet(KEY, fingerprints).commit()
        _fingerprints.value = fingerprints
    }

    private companion object {
        const val KEY = "fingerprints"
    }
}

/**
 * The clusters reached over a VPN only: without a VPN up the app does not try them (screens
 * say to connect it, background checks wait).
 */
class VpnOnlyClusters(prefs: SharedPreferences) : FingerprintSet(prefs) {
    companion object {
        const val FILE = "ichor-vpn-only"
    }
}

/** The clusters the background monitor leaves out ("Watch in the background" off; on by default). */
class UnwatchedClusters(prefs: SharedPreferences) : FingerprintSet(prefs) {
    companion object {
        const val FILE = "ichor-unwatched"
    }
}
