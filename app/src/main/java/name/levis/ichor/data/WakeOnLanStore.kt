package name.levis.ichor.data

import android.content.SharedPreferences
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import name.levis.ichor.model.ConfigSummary
import name.levis.ichor.model.WolTarget
import name.levis.ichor.model.decodeWolTarget
import name.levis.ichor.model.encodeWolTarget
import name.levis.ichor.model.keepWolTargets
import name.levis.ichor.model.wolKey

/**
 * How to wake each node (see [WolTarget]), by cluster fingerprint and node address: the
 * address is what the talosconfig names a node by, and all the app knows of one that is off.
 * Only on this device; forgotten with the cluster.
 */
class WakeOnLanStore(private val prefs: SharedPreferences) {
    private val _targets = MutableStateFlow(
        prefs.all.mapNotNull { (key, value) -> (value as? String)?.let(::decodeWolTarget)?.let { key to it } }.toMap(),
    )
    val targets: StateFlow<Map<String, WolTarget>> = _targets.asStateFlow()

    /** Saves how to wake [node]; a null [target] forgets it. */
    fun set(fingerprint: String, node: String, target: WolTarget?) {
        if (fingerprint.isBlank() || node.isBlank()) return
        val key = wolKey(fingerprint, node)
        store(if (target == null) _targets.value - key else _targets.value + (key to target))
    }

    /** Forgets the nodes of the clusters no longer in [summary]. */
    fun sync(summary: ConfigSummary) = store(keepWolTargets(_targets.value, summary.contexts.map { it.fingerprint }))

    private fun store(targets: Map<String, WolTarget>) {
        if (targets == _targets.value) return
        prefs.edit().clear().also { editor ->
            targets.forEach { (key, target) -> editor.putString(key, encodeWolTarget(target)) }
        }.apply()
        _targets.value = targets
    }

    companion object {
        const val FILE = "talosdev-mobile-wake-on-lan"
    }
}
