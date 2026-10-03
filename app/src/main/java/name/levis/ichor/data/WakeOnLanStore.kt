package name.levis.ichor.data

import android.content.SharedPreferences
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import name.levis.ichor.model.ConfigSummary
import name.levis.ichor.model.SeenMac
import name.levis.ichor.model.WolTarget
import name.levis.ichor.model.decodeSeenMacs
import name.levis.ichor.model.decodeWolTarget
import name.levis.ichor.model.encodeSeenMacs
import name.levis.ichor.model.encodeWolTarget
import name.levis.ichor.model.keepWolTargets
import name.levis.ichor.model.wolKey
import kotlinx.serialization.Serializable

/**
 * How to wake each node (see [WolTarget]), by cluster fingerprint and node address: the
 * address is what the talosconfig names a node by, and all the app knows of one that is off.
 * Also the MACs each node was last seen with, recorded while it was up, so one that goes
 * down before it was set up can still be woken. Only on this device, encrypted ([sealed]: node
 * addresses and MACs map the cluster's network); forgotten with the cluster.
 */
class WakeOnLanStore(private val sealed: SealedValue) {
    /** What [sealed] holds: each map's values in the formats of [encodeWolTarget] and [encodeSeenMacs]. */
    @Serializable
    private data class Stored(val targets: Map<String, String> = emptyMap(), val seen: Map<String, String> = emptyMap())

    private val stored = sealed.read()
        ?.let { runCatching { TalosJson.decodeFromString(Stored.serializer(), it) }.getOrNull() }
        ?: Stored()

    private val _targets = MutableStateFlow(stored.targets.mapNotNull { (key, value) -> decodeWolTarget(value)?.let { key to it } }.toMap())
    val targets: StateFlow<Map<String, WolTarget>> = _targets.asStateFlow()

    private val _seen = MutableStateFlow(
        stored.seen.mapNotNull { (key, value) -> decodeSeenMacs(value).takeIf { it.isNotEmpty() }?.let { key to it } }.toMap(),
    )
    val seen: StateFlow<Map<String, List<SeenMac>>> = _seen.asStateFlow()

    /** Nodes whose MACs were already looked at since the app started: once is enough. */
    private var looked = emptySet<String>()

    /** Saves how to wake [node]; a null [target] forgets it. */
    fun set(fingerprint: String, node: String, target: WolTarget?) {
        if (fingerprint.isBlank() || node.isBlank()) return
        val key = wolKey(fingerprint, node)
        store(if (target == null) _targets.value - key else _targets.value + (key to target))
    }

    /** Whether [node]'s MACs are still to be read since the app started: once is enough. */
    fun shouldRecord(fingerprint: String, node: String): Boolean =
        fingerprint.isNotBlank() && node.isNotBlank() && wolKey(fingerprint, node) !in looked

    /** Remembers the MACs [node] has now; an empty list (nothing physical seen) keeps the previous ones. */
    fun record(fingerprint: String, node: String, macs: List<SeenMac>) {
        if (fingerprint.isBlank() || node.isBlank()) return
        val key = wolKey(fingerprint, node)
        looked = looked + key
        if (macs.isNotEmpty()) storeSeen(_seen.value + (key to macs))
    }

    /** Forgets the nodes of the clusters no longer in [summary]. */
    fun sync(summary: ConfigSummary) {
        val fingerprints = summary.contexts.map { it.fingerprint }
        store(keepWolTargets(_targets.value, fingerprints))
        storeSeen(keepWolTargets(_seen.value, fingerprints))
    }

    private fun store(targets: Map<String, WolTarget>) {
        if (targets == _targets.value) return
        _targets.value = targets
        persist()
    }

    private fun storeSeen(seen: Map<String, List<SeenMac>>) {
        if (seen == _seen.value) return
        _seen.value = seen
        persist()
    }

    /** Best effort: with the Keystore unavailable, the change only lasts until the app stops. */
    private fun persist() {
        val stored = Stored(
            targets = _targets.value.mapValues { encodeWolTarget(it.value) },
            seen = _seen.value.mapValues { encodeSeenMacs(it.value) },
        )
        runCatching {
            if (stored == Stored()) sealed.delete() else sealed.write(TalosJson.encodeToString(Stored.serializer(), stored))
        }
    }

    companion object {
        /** Plaintext preferences of versions before encryption, moved by [migrate]. */
        const val FILE = "talosdev-mobile-wake-on-lan"
        const val SEEN_FILE = "talosdev-mobile-wake-on-lan-seen"

        /** Moves what older versions kept in plaintext [targets] and [seen] preferences into [sealed]. */
        fun migrate(sealed: SealedValue, targets: SharedPreferences, seen: SharedPreferences) {
            if (targets.all.isEmpty() && seen.all.isEmpty()) return
            val legacy = Stored(
                targets = targets.all.mapNotNull { (key, value) -> (value as? String)?.let { key to it } }.toMap(),
                seen = seen.all.mapNotNull { (key, value) -> (value as? String)?.let { key to it } }.toMap(),
            )
            if (sealed.read() == null && runCatching { sealed.write(TalosJson.encodeToString(Stored.serializer(), legacy)) }.isFailure) {
                return // Keystore unavailable: left where it is, moved on a later start
            }
            targets.edit().clear().commit()
            seen.edit().clear().commit()
        }
    }
}
