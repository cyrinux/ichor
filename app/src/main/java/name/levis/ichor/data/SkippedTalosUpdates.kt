package name.levis.ichor.data

import android.content.SharedPreferences
import kotlinx.coroutines.flow.StateFlow
import name.levis.ichor.model.ConfigSummary
import name.levis.ichor.model.keepClusterNames

/**
 * The Talos release ("v1.14.2") each cluster's update card was skipped for, by context
 * fingerprint: the card stays away until a newer one is out. Only on this device.
 */
class SkippedTalosUpdates(prefs: SharedPreferences) {
    private val map = FingerprintPrefsMap.strings(prefs)
    val versions: StateFlow<Map<String, String>> get() = map.values

    /** Forgets the releases skipped for the clusters no longer in [summary]. */
    fun sync(summary: ConfigSummary) = map.store(keepClusterNames(map.values.value, summary.contexts.map { it.fingerprint }))

    /** Skips [version] for the cluster [fingerprint]; null (or blank) offers every release again. */
    fun set(fingerprint: String, version: String?) = map.set(fingerprint, version?.trim()?.takeUnless { it.isEmpty() })

    companion object {
        const val FILE = "ichor-skipped-talos-updates"
    }
}
