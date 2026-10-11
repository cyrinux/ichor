package name.levis.ichor.model

import kotlinx.serialization.Serializable

/**
 * A node, pod, workload, CronJob, Argo CD or Flux app one cluster's last known state names
 * (the Go core's SearchSnapshots): [cluster] is its cluster id (the fingerprint when it has
 * none), [context] the context it was read with, [at] when it was read (unix millis).
 * [target] opens it like a share link; null when no share link can name it.
 */
@Serializable
data class SearchResult(
    val cluster: String,
    val context: String,
    /** node, pod, workload, cronjob, argo-app or flux-app. */
    val kind: String,
    val namespace: String = "",
    val name: String,
    /** "Deployment · 2/3 ready", "Pod · Running · node-a". */
    val detail: String = "",
    val at: Long = 0,
    val score: Double = 0.0,
    val target: ShareTarget? = null,
)

/** The best [results] of a search, and how many matched in all. */
@Serializable
data class SearchResults(val results: List<SearchResult> = emptyList(), val total: Int = 0)

/** One cluster's results, in the ranker's order. */
data class SearchGroup(val cluster: String, val context: String, val results: List<SearchResult>)

/** Read longer ago than this, a result says when it was last seen. */
const val SEARCH_STALE_MS = 5 * 60_000L

/** Whether [at] is old enough to say "last seen": an unknown time (0) says nothing. */
fun SearchResult.isStale(now: Long): Boolean = at > 0 && now - at > SEARCH_STALE_MS

/**
 * The results by cluster, the [activeCluster] first, then the others in the order their best
 * result ranks; each keeps the ranker's order.
 */
fun List<SearchResult>.groupedByCluster(activeCluster: String?): List<SearchGroup> {
    val groups = groupBy { it.cluster }.map { (cluster, results) -> SearchGroup(cluster, results.first().context, results) }
    return groups.sortedBy { it.cluster != activeCluster }
}

/** How many recent searches are kept. */
const val RECENT_SEARCHES = 10

/** The recent searches with [query] (trimmed) first, once whatever its case; a blank one changes nothing. */
fun List<String>.withRecent(query: String): List<String> {
    // One line: the recent searches are kept one per line.
    val trimmed = query.replace(Regex("\\s+"), " ").trim()
    if (trimmed.isEmpty()) return this
    return (listOf(trimmed) + filterNot { it.equals(trimmed, ignoreCase = true) }).take(RECENT_SEARCHES)
}
