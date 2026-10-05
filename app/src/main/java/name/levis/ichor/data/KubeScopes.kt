package name.levis.ichor.data

import android.content.SharedPreferences
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import name.levis.ichor.model.ConfigSummary
import name.levis.ichor.model.KubeScope
import name.levis.ichor.model.keepClusterNames

/**
 * The namespace the Kubernetes screen lists, picked by the user, by context fingerprint (see
 * [KubeScope.stored]: "" for every namespace). Typed namespaces, when the credentials cannot
 * list them, are kept the same way. Only on this device.
 */
class KubeScopes(private val prefs: SharedPreferences) {
    private val _scopes = MutableStateFlow(
        prefs.all.mapNotNull { (fingerprint, scope) -> (scope as? String)?.let { fingerprint to it } }.toMap(),
    )
    val scopes: StateFlow<Map<String, String>> = _scopes.asStateFlow()

    /** The scope remembered for the cluster [fingerprint], null when none was picked. */
    fun scope(fingerprint: String): KubeScope? = _scopes.value[fingerprint]?.let { KubeScope.fromStored(it) }

    /** Forgets the scopes of the clusters no longer in [summary]. */
    fun sync(summary: ConfigSummary) = store(keepClusterNames(_scopes.value, summary.contexts.map { it.fingerprint }))

    /** Remembers [scope] for the cluster [fingerprint]; one not chosen forgets it. */
    fun set(fingerprint: String, scope: KubeScope) {
        if (fingerprint.isBlank()) return
        val stored = scope.stored
        store(if (stored == null) _scopes.value - fingerprint else _scopes.value + (fingerprint to stored))
    }

    private fun store(scopes: Map<String, String>) {
        if (scopes == _scopes.value) return
        prefs.edit().clear().also { editor -> scopes.forEach { (fingerprint, scope) -> editor.putString(fingerprint, scope) } }.apply()
        _scopes.value = scopes
    }

    companion object {
        const val FILE = "ichor-kube-scopes"
    }
}
