package name.levis.ichor.data

import android.content.SharedPreferences
import kotlinx.coroutines.flow.StateFlow
import name.levis.ichor.model.ConfigSummary

/** One sealed file per cluster, by context fingerprint (see [ClusterSecureFiles]). */
interface ClusterFiles {
    /** Null when the cluster has none; throws when it cannot be read (Keystore, security key). */
    fun readBytes(fingerprint: String): ByteArray?

    fun writeBytes(fingerprint: String, bytes: ByteArray)

    /** Forgets the files of the clusters not in [fingerprints]. */
    fun sync(fingerprints: Collection<String>)
}

/** Ichorgo.historyAppend: the ring with one more record, or a throw (bad record, newer ring). */
typealias HistoryAppender = (ring: ByteArray?, recordJson: String, nowMillis: Long) -> ByteArray

/**
 * Each cluster's 30-day history ring (one record per monitor run), sealed in [files]: the Go
 * core owns the format, this store only keeps the bytes. Only on this device (not in backups).
 */
class HistoryStore(private val files: ClusterFiles, private val appender: HistoryAppender) {
    private val lock = Any()

    /** The cluster [fingerprint]'s ring; null when it has none yet or it cannot be read now. */
    fun ring(fingerprint: String): ByteArray? = synchronized(lock) { runCatching { files.readBytes(fingerprint) }.getOrNull() }

    /**
     * Adds one run's [recordJson] to the cluster [fingerprint]'s ring. Skipped (false), the stored
     * bytes left as they are, when the ring cannot be read now (it would be lost), when Go refuses
     * the record or the ring (written by a newer version), or when the sealed write fails.
     */
    fun append(fingerprint: String, recordJson: String, nowMillis: Long): Boolean = synchronized(lock) {
        val ring = runCatching { files.readBytes(fingerprint) }.getOrElse { return false }
        val next = runCatching { appender(ring, recordJson, nowMillis) }.getOrNull()
        if (next == null || next.isEmpty()) return false
        runCatching { files.writeBytes(fingerprint, next) }.isSuccess
    }

    /** Drops the rings of the clusters no longer in [fingerprints] (all of them with an empty list). */
    fun sync(fingerprints: Collection<String>) = synchronized(lock) {
        runCatching { files.sync(fingerprints.filter { it.isNotBlank() }) }
        Unit
    }
}

/**
 * When the user last looked at each cluster's home (epoch millis, by fingerprint): what the
 * "since you last looked" card starts from. Only on this device.
 */
class LastLooked(prefs: SharedPreferences) {
    private val map = FingerprintPrefsMap(prefs, { it as? Long }, { fingerprint, value -> putLong(fingerprint, value) })
    val times: StateFlow<Map<String, Long>> get() = map.values

    /**
     * Where the card of the cluster [fingerprint] starts: when the user last looked; on a first
     * visit [now] is recorded, so the card does not replay the whole ring.
     */
    fun start(fingerprint: String, now: Long): Long = map.values.value[fingerprint] ?: now.also { map.set(fingerprint, it) }

    /** The user looked at the cluster [fingerprint] at [at]; never moves back. */
    fun mark(fingerprint: String, at: Long) {
        if (at > (map.values.value[fingerprint] ?: 0L)) map.set(fingerprint, at)
    }

    /** Forgets the clusters no longer in [summary]. */
    fun sync(summary: ConfigSummary) {
        val known = summary.contexts.map { it.fingerprint }.filter { it.isNotBlank() }.toSet()
        map.store(map.values.value.filterKeys { it in known })
    }

    companion object {
        const val FILE = "ichor-history-last-looked"
    }
}
