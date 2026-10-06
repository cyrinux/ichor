package name.levis.ichor.data

import android.content.SharedPreferences
import kotlinx.coroutines.flow.StateFlow
import name.levis.ichor.model.ConfigSummary
import name.levis.ichor.model.assignClusterColors

/**
 * The main color of each cluster, by context fingerprint (not its name, which screenshot
 * mode masks). The app's palette is generated from the color of the cluster on screen, so
 * one always sees which cluster that is.
 */
class ClusterColors(prefs: SharedPreferences) {
    private val map = FingerprintPrefsMap.ints(prefs)
    val colors: StateFlow<Map<String, Int>> get() = map.values

    /** Gives the clusters of [summary] that have no color yet a free one, and forgets the removed ones. */
    fun sync(summary: ConfigSummary) = map.store(assignClusterColors(map.values.value, summary.contexts.map { it.fingerprint }))

    fun set(fingerprint: String, color: Int) = map.set(fingerprint, color)

    companion object {
        const val FILE = "ichor-cluster-colors"
    }
}
