package name.levis.ichor.data

import android.content.SharedPreferences
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import name.levis.ichor.model.ConfigSummary
import name.levis.ichor.model.keepVpnOnly

/**
 * The clusters reached over a VPN only, by context fingerprint: without a VPN up the app
 * does not try them (screens say to connect it, background checks wait). Only on this device.
 */
class VpnOnlyClusters(private val prefs: SharedPreferences) {
    private val _fingerprints = MutableStateFlow(prefs.getStringSet(KEY, null)?.toSet().orEmpty())
    val fingerprints: StateFlow<Set<String>> = _fingerprints.asStateFlow()

    fun set(fingerprint: String, vpnOnly: Boolean) {
        if (fingerprint.isBlank()) return
        store(if (vpnOnly) _fingerprints.value + fingerprint else _fingerprints.value - fingerprint)
    }

    /** Forgets the clusters no longer in [summary]. */
    fun sync(summary: ConfigSummary) = store(keepVpnOnly(_fingerprints.value, summary.contexts.map { it.fingerprint }))

    /** Committed synchronously: the worker reads it back at process start. */
    private fun store(fingerprints: Set<String>) {
        if (fingerprints == _fingerprints.value) return
        prefs.edit().putStringSet(KEY, fingerprints).commit()
        _fingerprints.value = fingerprints
    }

    companion object {
        const val FILE = "ichor-vpn-only"
        private const val KEY = "fingerprints"
    }
}
