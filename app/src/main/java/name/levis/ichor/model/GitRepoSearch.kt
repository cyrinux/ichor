package name.levis.ichor.model

import kotlinx.serialization.Serializable

/** Find in repository: `GitRepoSearch` (a snapshot of the repository) or `FluxSourceSearch` (a Flux artifact). */
@Serializable
data class GitRepoSearch(
    val sha: String = "",
    val hits: List<GitSearchHit> = emptyList(),
    /** The text files searched. */
    val searched: Int = 0,
    /** More lines match than [hits] holds. */
    val truncated: Boolean = false,
)

/** One matching line: [path] from the repository root, [line] 1-based, [text] trimmed to 200 characters. */
@Serializable
data class GitSearchHit(val path: String = "", val line: Int = 0, val text: String = "")

/** [hits] grouped by file, files in path order, as the results list shows them. */
fun groupSearchHits(hits: List<GitSearchHit>): List<Pair<String, List<GitSearchHit>>> =
    hits.groupBy { it.path }.toSortedMap().map { (path, list) -> path to list.sortedBy { it.line } }
