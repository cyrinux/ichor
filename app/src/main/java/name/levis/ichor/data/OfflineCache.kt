package name.levis.ichor.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import kotlinx.serialization.Serializable
import java.io.File
import java.security.KeyStore
import java.security.MessageDigest
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.atomic.AtomicInteger

/**
 * The last results fetched from each cluster, kept on disk when the user turned on "Keep last
 * known state" (off by default), so a screen still has something to show when the cluster
 * cannot be reached, even after the app was closed.
 *
 * One file per cluster and result, encrypted by [seal] (AES-GCM with a Keystore key in the
 * app), in a directory excluded from backups. File and directory names are hashes: no node
 * address or cluster name shows on disk. Results older than [MAX_AGE_MS] are dropped.
 */
class OfflineCache(
    private val directory: File,
    private val seal: Sealer,
    private val enabled: () -> Boolean,
    private val now: () -> Long = System::currentTimeMillis,
) {
    /** Encrypts what is written and decrypts what is read; [forget] drops its key. */
    interface Sealer {
        fun write(file: File, bytes: ByteArray)
        fun read(file: File): ByteArray?
        fun forget()
    }

    @Serializable
    private data class Entry(val key: String, val at: Long, val json: String)

    /** Clusters read from disk (by fingerprint), each a map of result key to its JSON. */
    private val loaded = ConcurrentHashMap<String, ConcurrentHashMap<String, TalosRepository.Timed<String>>>()

    /** Clusters whose files were read: a result saved before [load] must not hide the others. */
    private val read = ConcurrentHashMap.newKeySet<String>()

    /** Files and the key are touched one caller at a time: a wipe never races a write. */
    private val lock = Mutex()

    /**
     * Bumped by [clear]. A result fetched before is not saved after: it may carry real names
     * fetched before screenshot mode was turned on, or reach the disk once turned off.
     */
    private val generation = AtomicInteger()

    /** Pass to [save]: what [clear] has to bump for a save to be dropped. */
    val epoch: Int get() = generation.get()

    /** Reads the stored results of the cluster [fingerprint] once, so [peek] has them. Expired or unreadable files are deleted. */
    suspend fun load(fingerprint: String) = locked {
        if (!enabled() || fingerprint.isBlank() || !read.add(fingerprint)) return@locked
        val entries = loaded.getOrPut(fingerprint) { ConcurrentHashMap() }
        clusterDir(fingerprint).listFiles()?.forEach { file ->
            val entry = runCatching { seal.read(file)?.let { TalosJson.decodeFromString(Entry.serializer(), it.decodeToString()) } }.getOrNull()
            if (entry == null || expired(entry.at)) file.delete() else entries.putIfAbsent(entry.key, TalosRepository.Timed(entry.json, entry.at))
        }
    }

    /** The stored JSON of [key] for the cluster [fingerprint], once [load]ed; null if none or expired. */
    fun peek(fingerprint: String, key: String): TalosRepository.Timed<String>? {
        if (!enabled()) return null
        return loaded[fingerprint]?.get(key)?.takeUnless { expired(it.at) }
    }

    /**
     * Stores [json] as the result [key] of the cluster [fingerprint], fetched at [at]; dropped
     * when [clear] ran since [epoch] was read (before the fetch).
     */
    suspend fun save(fingerprint: String, key: String, json: String, at: Long, epoch: Int) = locked {
        if (!enabled() || fingerprint.isBlank() || epoch != generation.get()) return@locked
        val dir = clusterDir(fingerprint).apply { mkdirs() }
        seal.write(File(dir, hash(key)), TalosJson.encodeToString(Entry.serializer(), Entry(key, at, json)).encodeToByteArray())
        loaded.getOrPut(fingerprint) { ConcurrentHashMap() }[key] = TalosRepository.Timed(json, at)
    }

    /** Deletes what is stored for the clusters other than [fingerprints] (removed ones). */
    suspend fun retain(fingerprints: Collection<String>) = locked {
        val kept = fingerprints.map(::hash).toSet()
        directory.listFiles()?.filter { it.name !in kept }?.forEach { it.deleteRecursively() }
        loaded.keys.retainAll(fingerprints.toSet())
        read.retainAll(fingerprints.toSet())
    }

    /** Deletes everything stored and its key, e.g. when turned off or screenshot mode changed. */
    suspend fun clear() {
        // At once, before waiting for the lock: from now on nothing fetched earlier is kept or shown.
        generation.incrementAndGet()
        loaded.clear()
        locked {
            loaded.clear()
            read.clear()
            if (!directory.exists()) return@locked // nothing was ever kept: no key to drop either
            directory.deleteRecursively()
            // The files are gone: a key that cannot be deleted only protects nothing.
            runCatching { seal.forget() }
        }
    }

    private suspend fun locked(block: () -> Unit) = withContext(Dispatchers.IO) { lock.withLock { block() } }

    private fun expired(at: Long) = now() - at > MAX_AGE_MS

    private fun clusterDir(fingerprint: String) = File(directory, hash(fingerprint))

    companion object {
        /** Older results tell more about the past than about the cluster: they are dropped. */
        const val MAX_AGE_MS = 24 * 60 * 60 * 1000L

        private fun hash(text: String): String =
            MessageDigest.getInstance("SHA-256").digest(text.encodeToByteArray()).joinToString("") { "%02x".format(it) }
    }
}

/** [OfflineCache.Sealer] backed by [SecureStore]: one Keystore key for every file. */
class KeystoreSealer(private val keyAlias: String) : OfflineCache.Sealer {
    override fun write(file: File, bytes: ByteArray) = SecureStore(file, keyAlias).write(bytes)
    override fun read(file: File): ByteArray? = SecureStore(file, keyAlias).read()
    override fun forget() {
        KeyStore.getInstance("AndroidKeyStore").apply { load(null) }.deleteEntry(keyAlias)
    }
}
