package name.levis.ichor.data

import android.content.SharedPreferences
import java.util.concurrent.ConcurrentHashMap
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import name.levis.ichor.model.Pin
import name.levis.ichor.model.Pins

/**
 * The objects pinned to each cluster's overview, kept with the other UI preferences: names a
 * share link would carry, never a credential. [cluster] is a [pinScope].
 */
class PinStore(private val prefs: SharedPreferences) {
    private val flows = ConcurrentHashMap<String, MutableStateFlow<List<Pin>>>()

    fun pins(cluster: String): StateFlow<List<Pin>> = flow(cluster).asStateFlow()

    /** Pins or unpins [pin]'s object; false when the cluster already has [Pins.MAX] pins. */
    fun toggle(cluster: String, pin: Pin): Boolean {
        val flow = flow(cluster)
        val next = Pins.toggle(flow.value, pin) ?: return false
        prefs.edit().putString(KEY_PREFIX + cluster, Pins.encode(next)).apply()
        flow.value = next
        return true
    }

    private fun flow(cluster: String): MutableStateFlow<List<Pin>> =
        flows.getOrPut(cluster) { MutableStateFlow(Pins.parse(prefs.getString(KEY_PREFIX + cluster, null))) }

    companion object {
        private const val KEY_PREFIX = "pins."
    }
}

/**
 * Where a cluster's pins are kept: by its [fingerprint], and apart while screenshot mode is on.
 * A pin made then holds the masked names, one made before the real ones: they must never mix,
 * so a real hostname never shows on a masked overview.
 */
fun pinScope(fingerprint: String, mask: PrivacyMask): String =
    if (mask.enabled) "$fingerprint.${mask.storageKey}" else fingerprint
