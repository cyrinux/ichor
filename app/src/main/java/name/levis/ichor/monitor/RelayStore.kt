package name.levis.ichor.monitor

import android.content.Context
import android.content.SharedPreferences
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.serialization.Serializable
import kotlinx.serialization.builtins.MapSerializer
import kotlinx.serialization.builtins.serializer
import name.levis.ichor.TalosApp
import name.levis.ichor.data.TalosJson
import name.levis.ichor.model.PromSource

/**
 * One cluster's push from its ichor-relay: [enabled] by the user, [source] the relay found when
 * turned on; the distributor's [endpoint] and public keys ([p256dh], [auth]: not secrets, the
 * private keys stay in the UnifiedPush connector's own store) until [deviceId] says the relay
 * took them. [problem] says why it is not paired (see [RelayProblem]), [detail] the error text.
 */
@Serializable
data class RelayPair(
    val enabled: Boolean = false,
    val source: PromSource? = null,
    val label: String = "",
    val endpoint: String = "",
    val p256dh: String = "",
    val auth: String = "",
    val deviceId: String = "",
    val distributor: String = "",
    val pairedAt: Long = 0,
    val lastPushAt: Long = 0,
    val problem: String = "",
    val detail: String = "",
) {
    val paired: Boolean get() = enabled && deviceId.isNotEmpty()

    /** An endpoint the relay has not taken yet: registered again at the next background check. */
    val pending: Boolean get() = enabled && source != null && endpoint.isNotEmpty() && deviceId.isEmpty()
}

/** Why a [RelayPair] is not (or no longer) delivering. */
object RelayProblem {
    /** The relay refused or could not be reached; [RelayPair.detail] has its error. */
    const val REGISTER = "register"

    /** The distributor could not register ([RelayPair.detail]: the connector's FailedReason). */
    const val DISTRIBUTOR = "distributor"

    /** A message arrived the distributor could not decrypt: it is too old for Web Push. */
    const val UNDECRYPTED = "undecrypted"
}

/** The [RelayPair] of each cluster by fingerprint, in [prefs] as one JSON map; nothing secret. */
class RelayStore(private val prefs: SharedPreferences) {
    private val _pairs = MutableStateFlow(load())
    val pairs: StateFlow<Map<String, RelayPair>> = _pairs.asStateFlow()

    fun read(fingerprint: String): RelayPair = _pairs.value[fingerprint] ?: RelayPair()

    /** Changes the cluster's pair; a pair turned back to the default is dropped. */
    @Synchronized
    fun update(fingerprint: String, change: (RelayPair) -> RelayPair) {
        if (fingerprint.isBlank()) return
        val next = change(read(fingerprint))
        save(if (next == RelayPair()) _pairs.value - fingerprint else _pairs.value + (fingerprint to next))
    }

    fun clear(fingerprint: String) = update(fingerprint) { RelayPair() }

    /** Forgets the pairs of the clusters not among [fingerprints]: a removed cluster's goes with it. */
    @Synchronized
    fun retain(fingerprints: Collection<String>) {
        val kept = _pairs.value.filterKeys { it in fingerprints }
        if (kept.size != _pairs.value.size) save(kept)
    }

    private fun save(pairs: Map<String, RelayPair>) {
        prefs.edit().putString(KEY, TalosJson.encodeToString(SERIALIZER, pairs)).apply()
        _pairs.value = pairs
    }

    private fun load(): Map<String, RelayPair> =
        prefs.getString(KEY, null)?.let { runCatching { TalosJson.decodeFromString(SERIALIZER, it) }.getOrNull() }.orEmpty()

    companion object {
        const val FILE = "ichor-relay"
        private const val KEY = "pairs"
        private val SERIALIZER = MapSerializer(String.serializer(), RelayPair.serializer())

        @Volatile private var shared: RelayStore? = null

        /** The one store of the process, shared by the settings row, RelayPushService and the background check. */
        fun of(context: Context): RelayStore = shared ?: synchronized(this) {
            shared ?: RelayStore(context.applicationContext.getSharedPreferences(FILE, Context.MODE_PRIVATE)).also { shared = it }
        }
    }
}

/** Each cluster's push from its ichor-relay (UnifiedPush), see [RelayPushService]. */
val TalosApp.relayStore: RelayStore get() = RelayStore.of(this)
