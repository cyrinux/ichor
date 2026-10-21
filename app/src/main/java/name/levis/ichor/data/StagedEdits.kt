package name.levis.ichor.data

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import name.levis.ichor.model.GitProposalFile
import name.levis.ichor.model.GitSource
import name.levis.ichor.model.proposalBaseRef

/** At most this many files go in one pull request (the core refuses more). */
const val MAX_STAGED_FILES = 20

/** Where staged files go together: one repository, read and proposed at one branch. */
data class StagedKey(val repoURL: String, val baseRef: String)

/** The staging key of the repository [src], at the branch its files are read and proposed at. */
fun stagedKeyOf(src: GitSource): StagedKey = StagedKey(src.repoURL, proposalBaseRef(src.tracked) ?: src.ref)

/** One file staged for a pull request: [original] and its [fileSha] as read at the branch, [draft] the new text. */
data class StagedFile(val path: String, val original: String, val fileSha: String, val draft: String) {
    val proposal: GitProposalFile get() = GitProposalFile(path = path, content = draft, fileSha = fileSha)
}

/**
 * Edits staged in the repository browser, proposed later as one pull request: in memory for the
 * app's session only (lost when the app is killed), per repository and branch, one entry per
 * path (staging a file again replaces it).
 */
class StagedEdits {
    private val _files = MutableStateFlow<Map<StagedKey, List<StagedFile>>>(emptyMap())
    val files: StateFlow<Map<StagedKey, List<StagedFile>>> = _files.asStateFlow()

    /** The files staged for [key], in the order they were first staged. */
    fun of(key: StagedKey): List<StagedFile> = _files.value[key].orEmpty()

    /** Whether [path] can be staged for [key]: already there, or room for one more. */
    fun canStage(key: StagedKey, path: String): Boolean = of(key).let { list -> list.any { it.path == path } || list.size < MAX_STAGED_FILES }

    /** Stages [file] for [key], in place of an earlier stage of the same path. */
    fun stage(key: StagedKey, file: StagedFile) = _files.update { all ->
        val list = all[key].orEmpty()
        val at = list.indexOfFirst { it.path == file.path }
        all + (key to if (at < 0) list + file else list.mapIndexed { i, f -> if (i == at) file else f })
    }

    /** Drops [path] from [key]'s staged files. */
    fun remove(key: StagedKey, path: String) = _files.update { all ->
        val rest = all[key].orEmpty().filter { it.path != path }
        if (rest.isEmpty()) all - key else all + (key to rest)
    }

    /** Drops every file staged for [key] (they were proposed). */
    fun clear(key: StagedKey) = _files.update { it - key }
}

/**
 * The path a pull request of several [paths] is titled after: their deepest common directory,
 * else the first path (files at the root have none in common).
 */
fun stagedDefaultsPath(paths: List<String>): String {
    if (paths.size < 2) return paths.firstOrNull().orEmpty()
    val dirs = paths.map { it.split('/').dropLast(1) }
    val common = dirs.reduce { acc, d -> acc.zip(d).takeWhile { (a, b) -> a == b }.map { it.first } }
    return common.joinToString("/").ifEmpty { paths.first() }
}
