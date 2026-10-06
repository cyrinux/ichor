package name.levis.ichor.data

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.serialization.KSerializer

/**
 * The results [TalosRepository] keeps: in memory for the screens, on disk through [offline]
 * when the user turned it on. [scope] prefixes every key with what makes a result stale (the
 * config generation and the context); [cluster] is the fingerprint results may be kept
 * under on disk, null when they must not be (the demo).
 */
class ResultCache(
    private val offline: OfflineCache?,
    private val scope: () -> String,
    private val cluster: () -> String?,
) {

    /**
     * Last successful results in memory, so screens can show them instantly while refreshing.
     * Keys include the config generation and context, so importing or switching invalidates them.
     * Cluster data reaches the disk only through [offline], when the user turned it on.
     */
    private val cache = java.util.concurrent.ConcurrentHashMap<String, Any>()

    /** The last result of [key]: fetched in this process, or else the last known one [offline]. */
    @Suppress("UNCHECKED_CAST")
    fun <T> cached(key: String): TalosRepository.Timed<T>? = (cache[scope() + key] ?: restored(key)) as TalosRepository.Timed<T>?

    private fun restored(key: String, cluster: String? = cluster()): TalosRepository.Timed<Any>? {
        val serializer = PERSISTED[key.substringBefore('|')] ?: return null
        val stored = offline?.peek(cluster ?: return null, key) ?: return null
        // Stored by an older version whose model no longer decodes: as good as nothing.
        val value = runCatching { TalosJson.decodeFromString(serializer, stored.value) }.getOrNull() ?: return null
        return TalosRepository.Timed(value, stored.at)
    }

    private val _restores = MutableStateFlow(0)

    /**
     * Bumped once [restoreOffline] read what [offline] kept: a screen that started loading
     * before (e.g. right after switching cluster) asks [cached] again.
     */
    val restores: StateFlow<Int> = _restores.asStateFlow()

    /** Reads what [offline] kept of the active cluster, so [cached] has it before the first fetch. */
    suspend fun restoreOffline() {
        val cluster = cluster() ?: return
        offline?.load(cluster) ?: return
        _restores.update { it + 1 }
    }

    /** Bumped by [invalidate]; screens showing cluster data reload when it changes. */
    private val _invalidations = MutableStateFlow(0)
    val invalidations: StateFlow<Int> = _invalidations.asStateFlow()

    /**
     * Drops every cached result and asks visible screens to reload, e.g. after screenshot
     * mode changed, so no value fetched under the previous setting stays on screen.
     */
    fun invalidate() {
        cache.clear()
        _invalidations.value++
    }

    /** Forgets the in-memory result of [key] for what is on screen now. */
    fun forget(key: String) {
        cache.remove(scope() + key)
    }

    /** Forgets every in-memory result whose scoped key contains [fragment], whatever its scope. */
    fun forgetContaining(fragment: String) {
        cache.keys.removeAll { fragment in it }
    }

    /**
     * Runs [block] and keeps its result for the cluster that was active when it started; on
     * disk too when [persistable] says it describes the cluster.
     */
    suspend fun <T : Any> remember(
        key: String,
        persistable: (T) -> Boolean = { true },
        block: suspend (last: () -> TalosRepository.Timed<T>?) -> T,
    ): T {
        val scope = scope() + key
        val cluster = cluster()
        val epoch = offline?.epoch ?: 0
        // The last result of the cluster this fetch is for, even if another one is shown by now.
        @Suppress("UNCHECKED_CAST")
        val value = block { (cache[scope] ?: restored(key, cluster)) as TalosRepository.Timed<T>? }
        val at = System.currentTimeMillis()
        cache[scope] = TalosRepository.Timed(value, at)
        if (cluster != null && persistable(value)) persist(cluster, key, value, at, epoch)
        return value
    }

    /**
     * Keeps a result of [key] obtained later (a list loaded page by page) as [remember] does,
     * for the cluster active now: call it before the load starts, then call what it returns
     * only with a complete list (L12: the last known state is never a partial one).
     */
    fun keeper(key: String): suspend (Any) -> Unit {
        val scope = scope() + key
        val cluster = cluster()
        val epoch = offline?.epoch ?: 0
        return { value ->
            val at = System.currentTimeMillis()
            cache[scope] = TalosRepository.Timed(value, at)
            if (cluster != null) persist(cluster, key, value, at, epoch)
        }
    }

    private suspend fun persist(cluster: String, key: String, value: Any, at: Long, epoch: Int) {
        @Suppress("UNCHECKED_CAST")
        val serializer = PERSISTED[key.substringBefore('|')] as KSerializer<Any>? ?: return
        try {
            offline?.save(cluster, key, TalosJson.encodeToString(serializer, value), at, epoch)
        } catch (e: CancellationException) {
            throw e
        } catch (_: Exception) {
            // Not kept on disk (full, key gone): the screen still gets its fresh result.
        }
    }
}
