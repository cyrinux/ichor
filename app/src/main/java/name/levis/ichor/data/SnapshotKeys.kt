package name.levis.ichor.data

import android.content.SharedPreferences
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import name.levis.ichor.model.ConfigSummary
import name.levis.ichor.model.keepClusterNames

/**
 * The public keys (age or SSH, one per line) each cluster's etcd snapshots were last
 * encrypted for, by context fingerprint, so the next backup is one tap. Public keys are not
 * secret; only on this device.
 */
class SnapshotKeys(private val prefs: SharedPreferences) {
    private val _keys = MutableStateFlow(
        prefs.all.mapNotNull { (fingerprint, keys) -> (keys as? String)?.let { fingerprint to it } }.toMap(),
    )
    val keys: StateFlow<Map<String, String>> = _keys.asStateFlow()

    /** Forgets the keys of the clusters no longer in [summary]. */
    fun sync(summary: ConfigSummary) = store(keepClusterNames(_keys.value, summary.contexts.map { it.fingerprint }))

    /** Remembers the cluster [fingerprint]'s keys; blank forgets them. */
    fun set(fingerprint: String, keys: String) {
        if (fingerprint.isBlank()) return
        val trimmed = keys.trim()
        store(if (trimmed.isEmpty()) _keys.value - fingerprint else _keys.value + (fingerprint to trimmed))
    }

    private fun store(keys: Map<String, String>) {
        if (keys == _keys.value) return
        prefs.edit().clear().also { editor -> keys.forEach { (fingerprint, value) -> editor.putString(fingerprint, value) } }.apply()
        _keys.value = keys
    }

    companion object {
        const val FILE = "ichor-snapshot-keys"
    }
}
