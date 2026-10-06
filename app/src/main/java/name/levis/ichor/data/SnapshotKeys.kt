package name.levis.ichor.data

import android.content.SharedPreferences
import kotlinx.coroutines.flow.StateFlow
import name.levis.ichor.model.ConfigSummary
import name.levis.ichor.model.keepClusterNames

/**
 * The public keys (age or SSH, one per line) each cluster's etcd snapshots were last
 * encrypted for, by context fingerprint, so the next backup is one tap. Public keys are not
 * secret; only on this device.
 */
class SnapshotKeys(prefs: SharedPreferences) {
    private val map = FingerprintPrefsMap.strings(prefs)
    val keys: StateFlow<Map<String, String>> get() = map.values

    /** Forgets the keys of the clusters no longer in [summary]. */
    fun sync(summary: ConfigSummary) = map.store(keepClusterNames(map.values.value, summary.contexts.map { it.fingerprint }))

    /** Remembers the cluster [fingerprint]'s keys; blank forgets them. */
    fun set(fingerprint: String, keys: String) = map.set(fingerprint, keys.trim().takeUnless { it.isEmpty() })

    companion object {
        const val FILE = "ichor-snapshot-keys"
    }
}
