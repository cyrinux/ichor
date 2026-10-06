package name.levis.ichor.data

import android.content.SharedPreferences
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/**
 * One value per cluster, by context fingerprint, kept in its own [prefs] file and mirrored
 * in [values]. [read] recovers a stored value from the preferences' raw type; [write] puts
 * one. [commit] writes synchronously, for a worker that reads the file back at process start.
 */
class FingerprintPrefsMap<V : Any>(
    private val prefs: SharedPreferences,
    read: (Any?) -> V?,
    private val write: SharedPreferences.Editor.(String, V) -> Unit,
    private val commit: Boolean = false,
) {
    private val _values = MutableStateFlow(prefs.all.mapNotNull { (fingerprint, raw) -> read(raw)?.let { fingerprint to it } }.toMap())
    val values: StateFlow<Map<String, V>> = _values.asStateFlow()

    /** Replaces the whole map, when it changed. */
    fun store(values: Map<String, V>) {
        if (values == _values.value) return
        val editor = prefs.edit().clear()
        values.forEach { (fingerprint, value) -> editor.write(fingerprint, value) }
        if (commit) editor.commit() else editor.apply()
        _values.value = values
    }

    /** Sets the cluster [fingerprint]'s value, or forgets it with a null one. */
    fun set(fingerprint: String, value: V?) {
        if (fingerprint.isBlank()) return
        store(if (value == null) _values.value - fingerprint else _values.value + (fingerprint to value))
    }

    companion object {
        fun strings(prefs: SharedPreferences, commit: Boolean = false) =
            FingerprintPrefsMap(prefs, { it as? String }, { fingerprint, value -> putString(fingerprint, value) }, commit)

        fun ints(prefs: SharedPreferences) = FingerprintPrefsMap(prefs, { it as? Int }, { fingerprint, value -> putInt(fingerprint, value) })
    }
}
