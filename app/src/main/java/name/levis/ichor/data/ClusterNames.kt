package name.levis.ichor.data

import android.content.SharedPreferences
import kotlinx.coroutines.flow.StateFlow
import name.levis.ichor.model.ConfigSummary
import name.levis.ichor.model.keepClusterNames
import name.levis.ichor.model.normalizeClusterName

/**
 * The names the user gave clusters, by context fingerprint, when their talosconfig context
 * name is not a nice one. Only shown on this device: the talosconfig is left as it is, so
 * importing it again still updates the same cluster.
 */
class ClusterNames(prefs: SharedPreferences) {
    private val map = FingerprintPrefsMap.strings(prefs)
    val names: StateFlow<Map<String, String>> get() = map.values

    /** Forgets the names of the clusters no longer in [summary]. */
    fun sync(summary: ConfigSummary) = map.store(keepClusterNames(map.values.value, summary.contexts.map { it.fingerprint }))

    /** Names the cluster [fingerprint] [input]; a blank one goes back to its context name. */
    fun set(fingerprint: String, input: String) = map.set(fingerprint, normalizeClusterName(input))

    companion object {
        const val FILE = "ichor-cluster-names"
    }
}
