package name.levis.ichor.data

import android.content.SharedPreferences
import kotlinx.coroutines.flow.StateFlow
import name.levis.ichor.model.ConfigSummary
import name.levis.ichor.model.keepClusterTags
import name.levis.ichor.model.normalizeTags

/**
 * The tags the user gave clusters ("prod", "eu"), by context fingerprint. Only on this device
 * and in backups, never in the config. Stored comma-joined: a normalized tag has no comma.
 */
class ClusterTags(prefs: SharedPreferences) {
    private val map = FingerprintPrefsMap<List<String>>(
        prefs,
        { raw -> (raw as? String)?.let(::normalizeTags) },
        { fingerprint, value -> putString(fingerprint, value.joinToString(",")) },
    )
    val tags: StateFlow<Map<String, List<String>>> get() = map.values

    /** Forgets the tags of the clusters no longer in [summary]. */
    fun sync(summary: ConfigSummary) = map.store(keepClusterTags(map.values.value, summary.contexts.map { it.fingerprint }))

    /** Sets the tags of the context [fingerprint]; none forgets them. */
    fun set(fingerprint: String, tags: List<String>) = map.set(fingerprint, tags.ifEmpty { null })

    companion object {
        const val FILE = "ichor-cluster-tags"
    }
}
