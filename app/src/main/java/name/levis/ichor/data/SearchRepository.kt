package name.levis.ichor.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.KSerializer
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import name.levis.ichor.model.ArgoStatus
import name.levis.ichor.model.ConfigSummary
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.FluxStatus
import name.levis.ichor.model.ScreenEntries
import name.levis.ichor.model.ScreenEntry
import name.levis.ichor.model.ScreenMatches
import name.levis.ichor.model.SearchResults
import name.levis.ichor.model.isDemo
import name.levis.ichorgo.Ichorgo

/**
 * What a search runs over: the [entries] JSON the Go core ranks, built once when the search
 * opens (and after a refresh), so typing only ranks. [othersNeedOfflineCache]: other clusters
 * are not searched because "Keep last known state" is off.
 */
data class SearchSnapshot(val entries: String, val isEmpty: Boolean, val othersNeedOfflineCache: Boolean) {
    companion object {
        val EMPTY = SearchSnapshot("""{"entries":[]}""", isEmpty = true, othersNeedOfflineCache = false)
    }
}

/**
 * Search across clusters (U3): the nodes, pods, workloads, CronJobs, Argo CD and Flux apps
 * of every cluster's last known state ([offline]), the active cluster's results in memory
 * ([results]) and the demo's built-in ones, ranked by the Go core. Never calls a cluster.
 */
class SearchRepository(
    private val summary: () -> ConfigSummary?,
    private val active: () -> String?,
    private val offline: OfflineCache?,
    private val offlineEnabled: () -> Boolean,
    private val results: ResultCache,
    private val demoEntries: () -> String = { Ichorgo.demoSearchEntries() },
    private val rank: (entries: String, query: String, limit: Int) -> String = { entries, query, limit ->
        Ichorgo.searchSnapshots(entries, query, limit.toLong())
    },
    private val rankScreens: (screens: String, query: String, limit: Int) -> String = { screens, query, limit ->
        Ichorgo.searchScreens(screens, query, limit.toLong())
    },
) {
    @Serializable
    private data class Entry(val cluster: String, val context: String, val key: String, val at: Long, val json: String)

    @Serializable
    private data class Entries(val entries: List<Entry>)

    /**
     * Reads what every cluster on the phone last showed, the active one first; [fresh] lists
     * of the active cluster (by cache key) replace its cached ones (a refresh that did not
     * load a complete list, which is never kept).
     */
    suspend fun snapshot(fresh: Map<String, Any> = emptyMap()): SearchSnapshot = withContext(Dispatchers.IO) {
        val contexts = summary()?.contexts.orEmpty()
        val activeName = active()
        // One context per cluster (the active one, else the first, as a share link picks):
        // two contexts of a cluster would rank each of its pods twice.
        val searched = contexts.sortedBy { it.name != activeName }.distinctBy { it.clusterId.ifEmpty { "fp:" + it.fingerprint } }
        val entries = searched.flatMap { context ->
            when {
                context.isDemo -> demo(context)
                else -> stored(context, if (context.name == activeName) inMemory() + encoded(fresh) else emptyMap())
            }
        }
        SearchSnapshot(
            entries = SearchJson.encodeToString(Entries.serializer(), Entries(entries)),
            isEmpty = entries.isEmpty(),
            othersNeedOfflineCache = !offlineEnabled() && contexts.count { !it.isDemo } > 1,
        )
    }

    /** The best matches of [query] in [snapshot], at most [limit]; none for a blank query. */
    suspend fun search(snapshot: SearchSnapshot, query: String, limit: Int = 50): SearchResults {
        val words = query.trim().replace(WHITESPACE, " ")
        if (words.isEmpty()) return SearchResults()
        return withContext(Dispatchers.Default) {
            TalosJson.decodeFromString(SearchResults.serializer(), rank(snapshot.entries, words, limit))
        }
    }

    /** The best matches of [query] among the active cluster's [screens]; none for a blank query. */
    suspend fun screens(screens: List<ScreenEntry>, query: String, limit: Int = 20): ScreenMatches {
        val words = query.trim().replace(WHITESPACE, " ")
        if (words.isEmpty() || screens.isEmpty()) return ScreenMatches()
        return withContext(Dispatchers.Default) {
            val json = SearchJson.encodeToString(ScreenEntries.serializer(), ScreenEntries(screens))
            TalosJson.decodeFromString(ScreenMatches.serializer(), rankScreens(json, words, limit))
        }
    }

    /** The demo's built-in entries, as the context it is on this phone. */
    private fun demo(context: ContextSummary): List<Entry> =
        runCatching { TalosJson.decodeFromString(Entries.serializer(), demoEntries()).entries }.getOrDefault(emptyList())
            .map { it.copy(cluster = context.clusterId.ifEmpty { it.cluster }, context = context.name) }

    /** [context]'s last known results, [memory] (by key) over them. */
    private suspend fun stored(context: ContextSummary, memory: Map<String, TalosRepository.Timed<String>>): List<Entry> {
        val kept = if (offline != null && offlineEnabled() && context.fingerprint.isNotBlank()) {
            offline.load(context.fingerprint)
            offline.peekAll(context.fingerprint)
        } else {
            emptyMap()
        }
        val cluster = context.clusterId.ifEmpty { context.fingerprint }
        return (kept + memory).filterKeys { searched(it) }.withoutScopedLists().toSortedMap()
            .map { (key, timed) -> Entry(cluster, context.name, key, timed.at, timed.value) }
    }

    /** The active cluster's results in memory, as the JSON the cache keeps them in. */
    private fun inMemory(): Map<String, TalosRepository.Timed<String>> =
        results.cachedAll().filterKeys { searched(it) }.mapNotNull { (key, timed) ->
            encode(key, timed.value)?.let { key to TalosRepository.Timed(it, timed.at) }
        }.toMap()

    private fun encoded(fresh: Map<String, Any>): Map<String, TalosRepository.Timed<String>> {
        val now = System.currentTimeMillis()
        return fresh.mapNotNull { (key, value) -> encode(key, value)?.let { key to TalosRepository.Timed(it, now) } }.toMap()
    }

    private fun encode(key: String, value: Any): String? {
        @Suppress("UNCHECKED_CAST")
        val serializer = SERIALIZERS[key.substringBefore('|')] as KSerializer<Any>? ?: return null
        return runCatching { SearchJson.encodeToString(serializer, value) }.getOrNull()
    }

    private companion object {
        val WHITESPACE = Regex("\\s+")

        /** Defaults written out: the Go core reads a missing field as its zero value. */
        val SearchJson = Json(TalosJson) { encodeDefaults = true }

        /** What the Go core ranks, by key prefix. */
        val SERIALIZERS: Map<String, KSerializer<*>> =
            PERSISTED.filterKeys { it in setOf(OVERVIEW, PODS, WORKLOADS, CRON_JOBS) } +
                mapOf(ARGO_CD to ArgoStatus.serializer(), FLUX to FluxStatus.serializer())

        fun searched(key: String) = key.substringBefore('|') in SERIALIZERS

        /**
         * One list per kind, so nothing ranks twice: the list of every namespace ("pods|*"),
         * else the whole one a repository keeps under the bare key ("cronjobs"), else the
         * namespaces' ones.
         */
        fun <V> Map<String, V>.withoutScopedLists(): Map<String, V> = filterKeys { key ->
            val prefix = key.substringBefore('|')
            val all = "$prefix|*"
            when {
                all in this -> key == all
                prefix in this -> key == prefix
                else -> true
            }
        }
    }
}
