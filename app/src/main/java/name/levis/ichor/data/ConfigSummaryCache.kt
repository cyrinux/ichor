package name.levis.ichor.data

import kotlinx.coroutines.CancellationException
import kotlinx.serialization.Serializable
import name.levis.ichor.model.ConfigSummary
import name.levis.ichor.security.SecurityKeyRequiredException

/**
 * What a summary was made from: the digest of each stored text (Ichorgo.configDigest, "" for
 * a store left empty) and the screenshot mask's generation (Ichorgo.privacyGeneration, 0 while
 * the mask is off). A summary is current only for the exact same key.
 */
data class SummaryKey(val talosDigest: String, val kubeDigest: String, val maskGeneration: Long)

/** The record sealed on disk. */
@Serializable
internal data class CachedSummary(
    val talosDigest: String,
    val kubeDigest: String,
    val maskGeneration: Long,
    val summaryJson: String,
)

/**
 * The summary of the stored configs kept next to them, so a launch skips the Go parse of every
 * context when neither text nor the mask changed. It names contexts, endpoints and fingerprints:
 * [sealed] must be protected like the stores (same security-key DEK, excluded from backups).
 *
 * It is a cache only: a record that cannot be read or decoded is deleted and taken for none,
 * never reported as an unreadable config (the stores alone decide that).
 */
class ConfigSummaryCache(private val sealed: SealedValue) {

    /** The cached summary for [key], or null on a miss (none, stale, unreadable). */
    fun lookup(key: SummaryKey): ConfigSummary? {
        val record = readRecord() ?: return null
        if (record.talosDigest != key.talosDigest || record.kubeDigest != key.kubeDigest) return null
        if (record.maskGeneration != key.maskGeneration) return null
        return try {
            TalosJson.decodeFromString(ConfigSummary.serializer(), record.summaryJson)
        } catch (_: Exception) {
            discard()
            null
        }
    }

    /** Keeps [summary] as the one for [key]; a write that fails leaves no record behind. */
    fun store(key: SummaryKey, summary: ConfigSummary) {
        val record = CachedSummary(
            talosDigest = key.talosDigest,
            kubeDigest = key.kubeDigest,
            maskGeneration = key.maskGeneration,
            summaryJson = TalosJson.encodeToString(ConfigSummary.serializer(), summary),
        )
        try {
            sealed.write(TalosJson.encodeToString(CachedSummary.serializer(), record))
        } catch (e: CancellationException) {
            throw e
        } catch (_: Exception) {
            // An older record would be stale now: never leave one that a later lookup could match.
            discard()
        }
    }

    /** Rewrites the record for the current protection (see SecureStore.reseal), or drops it. */
    fun reseal() {
        try {
            sealed.reseal()
        } catch (e: CancellationException) {
            throw e
        } catch (_: Exception) {
            discard()
        }
    }

    fun clear() = discard()

    private fun readRecord(): CachedSummary? {
        val text = try {
            sealed.read()
        } catch (_: SecurityKeyRequiredException) {
            // Sealed and no key tapped yet: still valid, only not readable this early.
            return null
        } catch (e: CancellationException) {
            throw e
        } catch (_: Exception) {
            discard()
            return null
        } ?: return null
        return try {
            TalosJson.decodeFromString(CachedSummary.serializer(), text)
        } catch (_: Exception) {
            discard()
            null
        }
    }

    private fun discard() {
        runCatching { sealed.delete() }
    }

    companion object {
        const val FILE = "config-summary.enc"
        const val KEY_ALIAS = "config-summary"
    }
}
