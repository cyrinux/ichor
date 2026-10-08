package name.levis.ichor.data

import kotlinx.serialization.builtins.MapSerializer
import kotlinx.serialization.builtins.serializer
import name.levis.ichor.security.DekHolder
import name.levis.ichorgo.AuthStore
import java.io.File

/**
 * What the sign-ins of kubeconfig clusters keep (refresh tokens, keys the user typed), one
 * opaque state per cluster fingerprint, written by the Go core through [AuthStore]. One sealed
 * file holds them all, protected like the stored configs.
 *
 * Go calls [load] and [save] from its own threads: every access holds the lock. A file that
 * cannot be read is never overwritten: until it reads again, nothing is saved (the sign-ins
 * of this run stay in the core's memory).
 */
class KubeAuthStore(private val file: SealedValue) : AuthStore {
    /** The states as last read or written; null until the file could be read. */
    private var states: Map<String, String>? = null

    @Synchronized
    override fun load(key: String): String = read()?.get(key).orEmpty()

    @Synchronized
    override fun save(key: String, state: String) {
        val current = read() ?: return
        write(withAuthState(current, key, state))
    }

    /** Every stored state, by fingerprint, for a backup. */
    @Synchronized
    fun all(): Map<String, String> = read().orEmpty()

    /** Forgets the states of the clusters no longer stored ([fingerprints] are those left). */
    @Synchronized
    fun retain(fingerprints: Collection<String>) {
        val current = read() ?: return
        write(current.filterKeys { it in fingerprints })
    }

    /**
     * Puts restored states back (a backup's: secrets only), each where this device has none:
     * a sign-in made here is kept rather than replaced by one that needs signing in again.
     */
    @Synchronized
    fun restore(restored: Map<String, String>) {
        val current = read() ?: return
        write(restored.filterValues { it.isNotBlank() }.filterKeys { current[it].isNullOrEmpty() } + current)
    }

    /** Forgets every sign-in, with the file. */
    @Synchronized
    fun clear() {
        runCatching { file.delete() }
        states = emptyMap()
    }

    /** Seals or unseals the file for the security-key mode just set (see SecureStore.reseal). */
    @Synchronized
    fun reseal() {
        file.reseal()
    }

    private fun read(): Map<String, String>? {
        states?.let { return it }
        return runCatching { decodeAuthStates(file.read()) }.getOrNull()?.also { states = it }
    }

    private fun write(updated: Map<String, String>) {
        if (updated == states) return
        // Kept in memory even when the Keystore refuses the write: this run's sign-ins still work.
        states = updated
        runCatching { if (updated.isEmpty()) file.delete() else file.write(encodeAuthStates(updated)) }
    }

    companion object {
        const val FILE = "kubeauth.enc"
        const val KEY_ALIAS = "kubeauth"
    }
}

/** [SealedValue] in a [SecureStore] file whose read failures are reported, not taken for "nothing stored". */
class SecureStoreValue(file: File, keyAlias: String, strongBox: Boolean, outer: DekHolder? = null) : SealedValue {
    private val store = SecureStore(file, keyAlias, strongBox, outer)

    override fun read(): String? = store.read()?.decodeToString()
    override fun write(value: String) = store.write(value.encodeToByteArray())
    override fun delete() = store.clear()
    override fun reseal() = store.reseal()
}

private val STATES = MapSerializer(String.serializer(), String.serializer())

/** [states] with [key]'s state set to [state], or removed when it is blank. */
internal fun withAuthState(states: Map<String, String>, key: String, state: String): Map<String, String> = when {
    key.isBlank() -> states
    state.isBlank() -> states - key
    else -> states + (key to state)
}

/** The stored file's states; none for no file. */
internal fun decodeAuthStates(json: String?): Map<String, String> =
    if (json.isNullOrBlank()) emptyMap() else TalosJson.decodeFromString(STATES, json)

internal fun encodeAuthStates(states: Map<String, String>): String = TalosJson.encodeToString(STATES, states)
