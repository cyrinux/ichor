package name.levis.ichor.data

import android.content.SharedPreferences
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import name.levis.ichor.model.ConfigSummary
import name.levis.ichor.model.keepClusterNames
import name.levis.ichor.model.normalizeClusterName

/**
 * The names the user gave clusters, by context fingerprint, when their talosconfig context
 * name is not a nice one. Only shown on this device: the talosconfig is left as it is, so
 * importing it again still updates the same cluster.
 */
class ClusterNames(private val prefs: SharedPreferences) {
    private val _names = MutableStateFlow(
        prefs.all.mapNotNull { (fingerprint, name) -> (name as? String)?.let { fingerprint to it } }.toMap(),
    )
    val names: StateFlow<Map<String, String>> = _names.asStateFlow()

    /** Forgets the names of the clusters no longer in [summary]. */
    fun sync(summary: ConfigSummary) = store(keepClusterNames(_names.value, summary.contexts.map { it.fingerprint }))

    /** Names the cluster [fingerprint] [input]; a blank one goes back to its context name. */
    fun set(fingerprint: String, input: String) {
        if (fingerprint.isBlank()) return
        val name = normalizeClusterName(input)
        store(if (name == null) _names.value - fingerprint else _names.value + (fingerprint to name))
    }

    private fun store(names: Map<String, String>) {
        if (names == _names.value) return
        prefs.edit().clear().also { editor -> names.forEach { (fingerprint, name) -> editor.putString(fingerprint, name) } }.apply()
        _names.value = names
    }

    companion object {
        const val FILE = "talosdev-mobile-cluster-names"
    }
}
