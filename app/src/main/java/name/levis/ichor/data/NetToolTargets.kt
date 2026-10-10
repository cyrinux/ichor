package name.levis.ichor.data

import android.content.SharedPreferences
import kotlinx.coroutines.flow.StateFlow
import name.levis.ichor.model.ConfigSummary
import name.levis.ichor.model.keepClusterNames
import name.levis.ichor.model.rememberTarget

/**
 * The last targets the network tools ran against, by context fingerprint, newest first (at most
 * ten): offered as chips on the next run. Only on this device; not in the backup.
 */
class NetToolTargets(prefs: SharedPreferences) {
    private val map = FingerprintPrefsMap.strings(prefs)
    val targets: StateFlow<Map<String, String>> get() = map.values

    /** The cluster [fingerprint]'s targets, newest first. */
    fun of(fingerprint: String): List<String> = map.values.value[fingerprint]?.split('\n')?.filter { it.isNotBlank() }.orEmpty()

    /** [target] used on the cluster [fingerprint]: it moves to the front. */
    fun remember(fingerprint: String, target: String) = map.set(fingerprint, rememberTarget(of(fingerprint), target).joinToString("\n"))

    /** Forgets the targets of the clusters no longer in [summary]. */
    fun sync(summary: ConfigSummary) = map.store(keepClusterNames(map.values.value, summary.contexts.map { it.fingerprint }))

    companion object {
        const val FILE = "ichor-net-tool-targets"
    }
}
